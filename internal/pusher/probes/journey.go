package probes

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	journeyPlaceholder      = regexp.MustCompile(`\$\{([^{}]+)\}`)
	journeyExactPlaceholder = regexp.MustCompile(`^\$\{([^{}]+)\}$`)
)

type journeyState struct {
	vars *journeyVars
	grpc *grpcDescriptorCache
}

type journeyVars struct {
	values map[string]any
	target Target
}

func newJourneyState(target Target) *journeyState {
	return &journeyState{
		vars: newJourneyVars(target),
		grpc: newGRPCCache(),
	}
}

func newJourneyVars(target Target) *journeyVars {
	return &journeyVars{
		values: map[string]any{},
		target: target,
	}
}

func (r *Runner) executeJourney(ctx context.Context, target Target, result *Result) {
	result.Type = "journey"
	retries := target.Retries
	if retries < 0 {
		retries = 0
	}
	var lastSteps []StepResult
	var lastFailed string
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
				goto done
			case <-time.After(250 * time.Millisecond):
			}
		}
		attemptCtx := ctx
		var cancel context.CancelFunc
		if budget := journeyBudget(target); budget > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, budget)
		}
		lastSteps, lastFailed, lastErr = r.runJourneyOnce(attemptCtx, target)
		if cancel != nil {
			cancel()
		}
		if lastErr == nil {
			break
		}
	}
done:
	result.Steps = lastSteps
	result.FailedStep = lastFailed
	if lastErr != nil {
		result.Success = false
		result.Status = "down"
		result.Error = lastErr.Error()
	} else {
		result.Success = true
		result.Status = "up"
	}
	if len(lastSteps) > 0 {
		last := lastSteps[len(lastSteps)-1]
		result.StatusCode = last.StatusCode
		result.Path = last.Path
	}
}

func journeyBudget(target Target) time.Duration {
	total := time.Duration(0)
	for _, step := range target.Steps {
		timeout := step.Timeout
		if timeout <= 0 {
			timeout = target.Timeout
		}
		if timeout <= 0 {
			timeout = 3 * time.Second
		}
		total += timeout
	}
	if total <= 0 {
		total = 30 * time.Second
	}
	if total > 2*time.Minute {
		total = 2 * time.Minute
	}
	return total
}

func stepTimeout(step Step, target Target) time.Duration {
	if step.Timeout > 0 {
		return step.Timeout
	}
	if target.Timeout > 0 {
		return target.Timeout
	}
	return 3 * time.Second
}

func (r *Runner) runJourneyOnce(ctx context.Context, target Target) ([]StepResult, string, error) {
	state := newJourneyState(target)
	steps := make([]StepResult, 0, len(target.Steps))
	for _, step := range target.Steps {
		if err := ctx.Err(); err != nil {
			return steps, step.Name, err
		}
		stepCtx := ctx
		var cancel context.CancelFunc
		if timeout := stepTimeout(step, target); timeout > 0 {
			stepCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		outcome := r.runJourneyStep(stepCtx, target, step, state)
		if cancel != nil {
			cancel()
		}
		steps = append(steps, outcome)
		if !outcome.Success {
			return steps, step.Name, fmt.Errorf("step %q: %s", step.Name, outcome.Error)
		}
	}
	return steps, "", nil
}

func (r *Runner) runJourneyStep(ctx context.Context, base Target, step Step, state *journeyState) StepResult {
	started := time.Now()
	out := StepResult{Name: step.Name, Type: step.Type, Status: "down"}

	var (
		err         error
		body        []byte
		statusCode  int
		headerValue func(string) (string, bool)
	)

	switch step.Type {
	case "http", "https":
		out.Method = strings.ToUpper(firstNonEmpty(step.Method, http.MethodGet))
		result, stepErr := r.runHTTPStep(ctx, base, step, state.vars)
		err = stepErr
		out.Target = result.URL
		out.Path = result.Path
		statusCode = result.StatusCode
		body = result.Body
		headerValue = result.headerValue
	case "grpc":
		result, stepErr := r.runGRPCStep(ctx, base, step, state)
		err = stepErr
		out.Target = result.Target
		statusCode = int(result.Code)
		body = result.Body
		headerValue = result.headerValue
	default:
		err = fmt.Errorf("unsupported step type %q", step.Type)
	}
	if err == nil && step.ExpectBody != "" && !strings.Contains(string(body), step.ExpectBody) {
		err = fmt.Errorf("response body does not contain %q", step.ExpectBody)
	}
	if err == nil {
		err = checkExpectJSON(body, step.ExpectJSON)
	}

	out.DurationMs = time.Since(started).Milliseconds()
	out.StatusCode = statusCode
	if err != nil {
		out.Error = err.Error()
		return out
	}
	if len(step.Capture) > 0 {
		captured, captureErr := captureJourneyValues(step.Capture, body, headerValue, statusCode, state.vars)
		if captureErr != nil {
			out.Error = captureErr.Error()
			return out
		}
		out.Captured = captured
	}
	out.Success = true
	out.Status = "up"
	return out
}

type httpStepResult struct {
	URL         string
	Path        string
	StatusCode  int
	Body        []byte
	headerValue func(string) (string, bool)
}

func (r *Runner) runHTTPStep(ctx context.Context, base Target, step Step, vars *journeyVars) (httpStepResult, error) {
	requestURL, err := resolveHTTPURL(base, step, vars)
	if err != nil {
		return httpStepResult{}, err
	}
	method := strings.ToUpper(firstNonEmpty(step.Method, http.MethodGet))
	body, bodyIsJSON, err := encodeHTTPBody(step.Body, step.Patch, vars)
	if err != nil {
		return httpStepResult{URL: requestURL}, err
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, reader)
	if err != nil {
		return httpStepResult{URL: requestURL}, err
	}
	req.Header.Set("User-Agent", "guardian-probe/1")
	headers, err := expandStringMap(step.Headers, vars)
	if err != nil {
		return httpStepResult{URL: requestURL}, err
	}
	setHTTPHeaders(req, headers)
	if bodyIsJSON && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: step.Insecure}, //nolint:gosec
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: transport,
	}
	resp, err := client.Do(req)
	if err != nil {
		return httpStepResult{URL: requestURL}, err
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	path := req.URL.Path
	out := httpStepResult{
		URL:         requestURL,
		Path:        path,
		StatusCode:  resp.StatusCode,
		Body:        responseBody,
		headerValue: headerGetter(resp.Header),
	}
	if readErr != nil {
		return out, readErr
	}
	if !expectedStatus(resp.StatusCode, step.ExpectStatus) {
		return out, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	return out, nil
}

func headerGetter(header http.Header) func(string) (string, bool) {
	return func(name string) (string, bool) {
		values := header.Values(name)
		if len(values) == 0 {
			return "", false
		}
		return values[0], true
	}
}

func resolveHTTPURL(base Target, step Step, vars *journeyVars) (string, error) {
	if rawURL := strings.TrimSpace(step.URL); rawURL != "" && !strings.Contains(rawURL, "://") {
		// Accept a bare host:port in the url field. Load balancer outputs are
		// often host names without a scheme.
		if step.Address == "" {
			step.Address = rawURL
		}
		if step.Port == 0 {
			step.Port = portFromAddress(rawURL)
		}
		step.URL = ""
	}
	if baseURL := strings.TrimSpace(base.URL); baseURL != "" && !strings.Contains(baseURL, "://") {
		if base.Address == "" {
			base.Address = baseURL
		}
		if base.Port == 0 {
			base.Port = portFromAddress(baseURL)
		}
		base.URL = ""
	}
	path := strings.TrimSpace(step.Path)
	if path != "" {
		expanded, err := vars.expandString(path)
		if err != nil {
			return "", err
		}
		path = expanded
	}
	if rawURL := strings.TrimSpace(step.URL); rawURL != "" {
		expanded, err := vars.expandString(rawURL)
		if err != nil {
			return "", err
		}
		parsed, err := url.Parse(expanded)
		if err != nil {
			return "", fmt.Errorf("parse step url: %w", err)
		}
		if path != "" {
			applyURLPath(parsed, path)
		}
		return parsed.String(), nil
	}
	if baseURL := strings.TrimSpace(base.URL); baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil {
			return "", fmt.Errorf("parse probe url: %w", err)
		}
		if path == "" {
			path = base.Path
		}
		if path != "" {
			applyURLPath(parsed, path)
		}
		return parsed.String(), nil
	}
	address := firstNonEmpty(step.Address, base.Address)
	if address == "" {
		return "", fmt.Errorf("step requires url or address")
	}
	expanded, err := vars.expandString(address)
	if err != nil {
		return "", err
	}
	address = expanded
	port := step.Port
	if port == 0 {
		port = base.Port
	}
	scheme := "http"
	if strings.EqualFold(step.Type, "https") || strings.EqualFold(base.Type, "https") {
		scheme = "https"
	}
	if path == "" {
		path = base.Path
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		if base.Path != "" && base.Path != "/" {
			path = strings.TrimSuffix(base.Path, "/") + "/" + path
		} else {
			path = "/" + path
		}
	}
	host := address
	if port > 0 && !strings.Contains(address, ":") {
		host = net.JoinHostPort(address, strconv.Itoa(port))
	}
	return scheme + "://" + host + path, nil
}

func applyURLPath(parsed *url.URL, path string) {
	query := ""
	if strings.Contains(path, "?") {
		parts := strings.SplitN(path, "?", 2)
		path = parts[0]
		query = parts[1]
	}
	if path != "" && !strings.HasPrefix(path, "/") {
		if parsed.Path != "" && parsed.Path != "/" {
			parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/" + path
		} else {
			parsed.Path = "/" + path
		}
	} else {
		parsed.Path = path
	}
	if query != "" {
		parsed.RawQuery = query
	}
}

func setHTTPHeaders(req *http.Request, headers map[string]string) {
	for key, value := range headers {
		if strings.EqualFold(key, "Host") {
			req.Host = value
			continue
		}
		req.Header.Set(key, value)
	}
}

func expandStringMap(values map[string]string, vars *journeyVars) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		expanded, err := vars.expandString(value)
		if err != nil {
			return nil, fmt.Errorf("header %s: %w", key, err)
		}
		out[key] = expanded
	}
	return out, nil
}

func (v *journeyVars) expandString(value string) (string, error) {
	var firstErr error
	out := journeyPlaceholder.ReplaceAllStringFunc(value, func(match string) string {
		if firstErr != nil {
			return match
		}
		expression := strings.TrimSpace(match[2 : len(match)-1])
		resolved, err := v.lookup(expression)
		if err != nil {
			firstErr = err
			return match
		}
		return stringifyJourneyValue(resolved)
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

func (v *journeyVars) expandValue(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		if match := journeyExactPlaceholder.FindStringSubmatch(typed); match != nil {
			return v.lookup(strings.TrimSpace(match[1]))
		}
		return v.expandString(typed)
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			expanded, err := v.expandValue(item)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			out[key] = expanded
		}
		return out, nil
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			expanded, err := v.expandString(item)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			out[key] = expanded
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for idx, item := range typed {
			expanded, err := v.expandValue(item)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", idx, err)
			}
			out[idx] = expanded
		}
		return out, nil
	default:
		return value, nil
	}
}

func (v *journeyVars) lookup(expression string) (any, error) {
	expression = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(expression), "$"))
	if expression == "" {
		return nil, fmt.Errorf("empty variable expression")
	}
	if strings.HasPrefix(expression, "target.") {
		return v.lookupTarget(strings.TrimPrefix(expression, "target."))
	}
	parts := splitJourneyPath(expression)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty variable expression")
	}
	value, ok := v.values[parts[0]]
	if !ok {
		return nil, fmt.Errorf("unknown variable %q", parts[0])
	}
	for _, part := range parts[1:] {
		next, ok := descendJourneyValue(value, part)
		if !ok {
			return nil, fmt.Errorf("variable %q has no value %q", expression, part)
		}
		value = next
	}
	return value, nil
}

func (v *journeyVars) lookupTarget(field string) (any, error) {
	switch strings.TrimSpace(field) {
	case "url":
		return v.target.URL, nil
	case "address":
		return v.target.Address, nil
	case "port":
		return v.target.Port, nil
	case "host":
		return probeHost(v.target), nil
	case "scheme":
		return probeScheme(v.target), nil
	case "name":
		return v.target.Name, nil
	case "service":
		return v.target.Service, nil
	case "asset":
		return v.target.Asset, nil
	default:
		return nil, fmt.Errorf("unknown target variable %q", field)
	}
}

func probeHost(target Target) string {
	if target.URL != "" {
		if parsed, err := url.Parse(target.URL); err == nil {
			return parsed.Hostname()
		}
	}
	host, _, err := net.SplitHostPort(target.Address)
	if err == nil {
		return host
	}
	return target.Address
}

func probeScheme(target Target) string {
	if target.URL != "" {
		if parsed, err := url.Parse(target.URL); err == nil && parsed.Scheme != "" {
			return parsed.Scheme
		}
	}
	if strings.EqualFold(target.Type, "https") {
		return "https"
	}
	return "http"
}

func splitJourneyPath(path string) []string {
	path = strings.ReplaceAll(path, "[", ".")
	path = strings.ReplaceAll(path, "]", "")
	raw := strings.Split(path, ".")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func descendJourneyValue(value any, part string) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		next, ok := typed[part]
		return next, ok
	case map[string]string:
		next, ok := typed[part]
		return next, ok
	case []any:
		index, err := strconv.Atoi(part)
		if err != nil || index < 0 || index >= len(typed) {
			return nil, false
		}
		return typed[index], true
	default:
		return nil, false
	}
}

func stringifyJourneyValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		if payload, err := json.Marshal(typed); err == nil {
			return string(payload)
		}
		return fmt.Sprint(typed)
	}
}

func encodeHTTPBody(body any, patch map[string]any, vars *journeyVars) ([]byte, bool, error) {
	if body == nil && patch == nil {
		return nil, false, nil
	}
	if vars == nil {
		vars = newJourneyVars(Target{})
	}
	expanded, err := vars.expandValue(body)
	if err != nil {
		return nil, false, err
	}
	if patch == nil {
		if text, ok := expanded.(string); ok {
			return []byte(text), false, nil
		}
		if expanded == nil {
			return nil, false, nil
		}
		payload, err := json.Marshal(expanded)
		if err != nil {
			return nil, false, err
		}
		return payload, true, nil
	}
	expandedPatch, err := vars.expandValue(patch)
	if err != nil {
		return nil, false, err
	}
	patchMap, ok := expandedPatch.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("patch must be a JSON object")
	}
	base := any(map[string]any{})
	switch typed := expanded.(type) {
	case nil:
	case string:
		if strings.TrimSpace(typed) == "" {
			break
		}
		if err := json.Unmarshal([]byte(typed), &base); err != nil {
			return nil, false, fmt.Errorf("patch requires a JSON body: %w", err)
		}
	default:
		base = typed
	}
	merged := applyMergePatch(base, patchMap)
	payload, err := json.Marshal(merged)
	if err != nil {
		return nil, false, err
	}
	return payload, true, nil
}

func encodeGRPCRequest(request any, patch map[string]any, vars *journeyVars) ([]byte, error) {
	payload, _, err := encodeHTTPBody(request, patch, vars)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func applyMergePatch(base any, patch map[string]any) any {
	target, ok := base.(map[string]any)
	if !ok {
		target = map[string]any{}
	}
	for key, value := range patch {
		if value == nil {
			delete(target, key)
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			target[key] = applyMergePatch(target[key], nested)
			continue
		}
		target[key] = value
	}
	return target
}

func captureJourneyValues(captures map[string]string, body []byte, headerValue func(string) (string, bool), statusCode int, vars *journeyVars) ([]string, error) {
	names := make([]string, 0, len(captures))
	var (
		parsed    any
		parsedSet bool
		parsedErr error
	)
	jsonBody := func() (any, error) {
		if !parsedSet {
			parsedSet = true
			if len(body) > 0 {
				parsedErr = json.Unmarshal(body, &parsed)
			}
		}
		return parsed, parsedErr
	}
	for key, expression := range captures {
		value, err := extractJourneyCapture(expression, body, headerValue, statusCode, jsonBody)
		if err != nil {
			return names, fmt.Errorf("capture %q: %w", key, err)
		}
		vars.values[key] = value
		names = append(names, key)
	}
	sort.Strings(names)
	return names, nil
}

func extractJourneyCapture(expression string, body []byte, headerValue func(string) (string, bool), statusCode int, jsonBody func() (any, error)) (any, error) {
	expression = strings.TrimSpace(expression)
	lower := strings.ToLower(expression)
	switch {
	case lower == "status":
		return statusCode, nil
	case lower == "body":
		return string(body), nil
	case lower == "json":
		return jsonBody()
	case strings.HasPrefix(lower, "json:"):
		return lookupJourneyJSON(jsonBody, strings.TrimSpace(expression[len("json:"):]))
	case strings.HasPrefix(lower, "header:"):
		name := strings.TrimSpace(expression[len("header:"):])
		return captureHeader(name, headerValue)
	case strings.HasPrefix(lower, "header."):
		name := strings.TrimSpace(expression[len("header."):])
		return captureHeader(name, headerValue)
	default:
		return lookupJourneyJSON(jsonBody, expression)
	}
}

func captureHeader(name string, headerValue func(string) (string, bool)) (string, error) {
	if name == "" {
		return "", fmt.Errorf("header capture requires a header name")
	}
	if headerValue == nil {
		return "", fmt.Errorf("header %q is not available", name)
	}
	value, ok := headerValue(name)
	if !ok {
		return "", fmt.Errorf("response header %q is missing", name)
	}
	return value, nil
}

func lookupJourneyJSON(jsonBody func() (any, error), path string) (any, error) {
	value, err := jsonBody()
	if err != nil {
		return nil, fmt.Errorf("parse JSON response: %w", err)
	}
	return lookupJourneyPath(value, path)
}

func lookupJourneyPath(value any, path string) (any, error) {
	parts := splitJourneyPath(strings.TrimPrefix(strings.TrimSpace(path), "$"))
	if len(parts) == 0 {
		return value, nil
	}
	current := value
	for _, part := range parts {
		next, ok := descendJourneyValue(current, part)
		if !ok {
			return nil, fmt.Errorf("JSON path %q not found", path)
		}
		current = next
	}
	return current, nil
}

func checkExpectJSON(body []byte, expectations map[string]any) error {
	if len(expectations) == 0 {
		return nil
	}
	var parsed any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &parsed); err != nil {
			return fmt.Errorf("response is not JSON: %w", err)
		}
	}
	for path, expected := range expectations {
		actual, err := lookupJourneyPath(parsed, path)
		if err != nil {
			return fmt.Errorf("expectJson %s: %w", path, err)
		}
		if stringifyJourneyValue(actual) != stringifyJourneyValue(expected) {
			return fmt.Errorf("expectJson %s = %s, want %s", path, stringifyJourneyValue(actual), stringifyJourneyValue(expected))
		}
	}
	return nil
}
