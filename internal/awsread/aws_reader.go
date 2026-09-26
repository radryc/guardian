package awsread

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	awsroot "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/xray"
)

const (
	defaultInsightsQuery = "fields @timestamp, @message | sort @timestamp desc"
	listMetricsPageLimit = 10
	insightsPollInterval = 700 * time.Millisecond
	insightsMaxWait      = 25 * time.Second
)

// Config configures the AWS reader.
type Config struct {
	Profile       string
	DefaultRegion string
	Regions       []string
	Account       string
}

// AWSReader implements Reader against the AWS SDK.
type AWSReader struct {
	account       string
	defaultRegion string
	regions       []string
	profile       string

	mu   sync.Mutex
	cfgs map[string]awsroot.Config
}

// NewAWSReader builds an SDK-backed reader.
func NewAWSReader(cfg Config) *AWSReader {
	defaultRegion := strings.TrimSpace(cfg.DefaultRegion)
	if defaultRegion == "" {
		defaultRegion = "us-east-1"
	}
	regions := make([]string, 0, len(cfg.Regions))
	for _, region := range cfg.Regions {
		if trimmed := strings.TrimSpace(region); trimmed != "" {
			regions = append(regions, trimmed)
		}
	}
	return &AWSReader{
		account:       strings.TrimSpace(cfg.Account),
		defaultRegion: defaultRegion,
		regions:       regions,
		profile:       strings.TrimSpace(cfg.Profile),
		cfgs:          make(map[string]awsroot.Config),
	}
}

func (r *AWSReader) loadConfig(ctx context.Context, region string) (awsroot.Config, error) {
	if region == "" {
		region = r.defaultRegion
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cfg, ok := r.cfgs[region]; ok {
		return cfg, nil
	}
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if r.profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(r.profile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return awsroot.Config{}, err
	}
	cfg.Credentials = awsroot.NewCredentialsCache(cfg.Credentials)
	r.cfgs[region] = cfg
	return cfg, nil
}

func (r *AWSReader) targetRegions() []string {
	if len(r.regions) > 0 {
		return r.regions
	}
	return []string{r.defaultRegion}
}

// ListMetrics discovers CloudWatch metrics, optionally filtered by namespace and
// metric name.
func (r *AWSReader) ListMetrics(ctx context.Context, namespace, metricName string) ([]MetricDescriptor, error) {
	out := make([]MetricDescriptor, 0)
	for _, region := range r.targetRegions() {
		cfg, err := r.loadConfig(ctx, region)
		if err != nil {
			return nil, err
		}
		client := cloudwatch.NewFromConfig(cfg)
		input := &cloudwatch.ListMetricsInput{}
		if namespace != "" {
			input.Namespace = awsroot.String(namespace)
		}
		if metricName != "" {
			input.MetricName = awsroot.String(metricName)
		}
		pages := 0
		for {
			resp, err := client.ListMetrics(ctx, input)
			if err != nil {
				return nil, fmt.Errorf("cloudwatch ListMetrics (%s): %w", region, err)
			}
			for _, metric := range resp.Metrics {
				dimensions := make([]string, 0, len(metric.Dimensions))
				for _, dim := range metric.Dimensions {
					if dim.Name != nil {
						dimensions = append(dimensions, *dim.Name)
					}
				}
				sort.Strings(dimensions)
				out = append(out, MetricDescriptor{
					Namespace:  awsroot.ToString(metric.Namespace),
					MetricName: awsroot.ToString(metric.MetricName),
					Dimensions: dimensions,
					Region:     region,
				})
			}
			pages++
			if resp.NextToken == nil || *resp.NextToken == "" || pages >= listMetricsPageLimit {
				break
			}
			input.NextToken = resp.NextToken
		}
	}
	return out, nil
}

// QueryMetric fetches a single CloudWatch metric series.
func (r *AWSReader) QueryMetric(ctx context.Context, q MetricQuery) ([]MetricSeries, error) {
	if strings.TrimSpace(q.MetricName) == "" || strings.TrimSpace(q.Namespace) == "" {
		return nil, fmt.Errorf("namespace and metric name are required")
	}
	stat := strings.TrimSpace(q.Stat)
	if stat == "" {
		stat = "Average"
	}
	period := int32(q.Period / time.Second)
	if period <= 0 {
		period = 300
	}
	from := q.From
	if from.IsZero() {
		from = time.Now().UTC().Add(-time.Hour)
	}
	to := q.To
	if to.IsZero() {
		to = time.Now().UTC()
	}

	dimensions := make([]cwtypes.Dimension, 0, len(q.Dimensions))
	for name, value := range q.Dimensions {
		if name == "" || value == "" {
			continue
		}
		dimensions = append(dimensions, cwtypes.Dimension{
			Name:  awsroot.String(name),
			Value: awsroot.String(value),
		})
	}

	out := make([]MetricSeries, 0)
	for _, region := range r.targetRegions() {
		cfg, err := r.loadConfig(ctx, region)
		if err != nil {
			return nil, err
		}
		client := cloudwatch.NewFromConfig(cfg)
		resp, err := client.GetMetricData(ctx, &cloudwatch.GetMetricDataInput{
			StartTime: awsroot.Time(from),
			EndTime:   awsroot.Time(to),
			MetricDataQueries: []cwtypes.MetricDataQuery{{
				Id: awsroot.String("m0"),
				MetricStat: &cwtypes.MetricStat{
					Metric: &cwtypes.Metric{
						Namespace:  awsroot.String(q.Namespace),
						MetricName: awsroot.String(q.MetricName),
						Dimensions: dimensions,
					},
					Period: awsroot.Int32(period),
					Stat:   awsroot.String(stat),
				},
				ReturnData: awsroot.Bool(true),
			}},
		})
		if err != nil {
			return nil, fmt.Errorf("cloudwatch GetMetricData (%s): %w", region, err)
		}
		for _, result := range resp.MetricDataResults {
			points := make([]MetricPoint, 0, len(result.Timestamps))
			for i, ts := range result.Timestamps {
				if i >= len(result.Values) {
					break
				}
				points = append(points, MetricPoint{Timestamp: ts.UTC(), Value: result.Values[i]})
			}
			sort.Slice(points, func(i, j int) bool { return points[i].Timestamp.Before(points[j].Timestamp) })
			out = append(out, MetricSeries{
				Namespace:  q.Namespace,
				MetricName: q.MetricName,
				Dimensions: q.Dimensions,
				Region:     region,
				Account:    r.account,
				Points:     points,
			})
		}
	}
	return out, nil
}

// ListLogGroups returns known CloudWatch log group names.
func (r *AWSReader) ListLogGroups(ctx context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	for _, region := range r.targetRegions() {
		cfg, err := r.loadConfig(ctx, region)
		if err != nil {
			return nil, err
		}
		client := cloudwatchlogs.NewFromConfig(cfg)
		paginator := cloudwatchlogs.NewDescribeLogGroupsPaginator(client, &cloudwatchlogs.DescribeLogGroupsInput{})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("logs DescribeLogGroups (%s): %w", region, err)
			}
			for _, group := range page.LogGroups {
				if name := awsroot.ToString(group.LogGroupName); name != "" {
					seen[name] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// QueryLogs runs a CloudWatch Logs Insights query and returns the rows.
func (r *AWSReader) QueryLogs(ctx context.Context, q LogQuery) ([]LogRecord, error) {
	if len(q.LogGroups) == 0 {
		return nil, fmt.Errorf("at least one log group is required")
	}
	queryString := strings.TrimSpace(q.Query)
	if queryString == "" {
		queryString = defaultInsightsQuery
	}
	limit := int32(q.Limit)
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	from := q.From
	if from.IsZero() {
		from = time.Now().UTC().Add(-time.Hour)
	}
	to := q.To
	if to.IsZero() {
		to = time.Now().UTC()
	}

	out := make([]LogRecord, 0)
	for _, region := range r.targetRegions() {
		cfg, err := r.loadConfig(ctx, region)
		if err != nil {
			return nil, err
		}
		client := cloudwatchlogs.NewFromConfig(cfg)
		started, err := client.StartQuery(ctx, &cloudwatchlogs.StartQueryInput{
			LogGroupNames: q.LogGroups,
			StartTime:     awsroot.Int64(from.UnixMilli()),
			EndTime:       awsroot.Int64(to.UnixMilli()),
			QueryString:   awsroot.String(queryString),
			Limit:         awsroot.Int32(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("logs StartQuery (%s): %w", region, err)
		}
		queryID := awsroot.ToString(started.QueryId)
		if queryID == "" {
			return nil, fmt.Errorf("logs StartQuery (%s) returned no query id", region)
		}
		records, err := r.pollInsights(ctx, client, queryID)
		if err != nil {
			return nil, err
		}
		out = append(out, records...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.After(out[j].Timestamp) })
	if len(out) > int(limit) {
		out = out[:limit]
	}
	return out, nil
}

func (r *AWSReader) pollInsights(ctx context.Context, client *cloudwatchlogs.Client, queryID string) ([]LogRecord, error) {
	deadline := time.Now().Add(insightsMaxWait)
	for {
		resp, err := client.GetQueryResults(ctx, &cloudwatchlogs.GetQueryResultsInput{QueryId: awsroot.String(queryID)})
		if err != nil {
			return nil, fmt.Errorf("logs GetQueryResults: %w", err)
		}
		switch resp.Status {
		case cloudwatchlogstypes.QueryStatusComplete:
			return insightsRecords(resp.Results), nil
		case cloudwatchlogstypes.QueryStatusFailed, cloudwatchlogstypes.QueryStatusCancelled, cloudwatchlogstypes.QueryStatusTimeout:
			return nil, fmt.Errorf("logs insights query %s ended with status %s", queryID, resp.Status)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("logs insights query %s timed out", queryID)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(insightsPollInterval):
		}
	}
}

func insightsRecords(rows [][]cloudwatchlogstypes.ResultField) []LogRecord {
	out := make([]LogRecord, 0, len(rows))
	for _, row := range rows {
		record := LogRecord{Fields: make(map[string]string, len(row))}
		for _, field := range row {
			name := awsroot.ToString(field.Field)
			value := awsroot.ToString(field.Value)
			switch name {
			case "@timestamp":
				if ts, err := parseInsightsTime(value); err == nil {
					record.Timestamp = ts
				}
			case "@message":
				record.Message = value
			case "@log_group":
				record.LogGroup = value
			case "@log_stream":
				record.LogStream = value
			default:
				record.Fields[name] = value
			}
		}
		out = append(out, record)
	}
	return out
}

func parseInsightsTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	if millis, err := strconv.ParseFloat(value, 64); err == nil {
		return time.UnixMilli(int64(millis)).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp %q", value)
}

// ListServices returns X-Ray service names.
func (r *AWSReader) ListServices(ctx context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	for _, region := range r.targetRegions() {
		cfg, err := r.loadConfig(ctx, region)
		if err != nil {
			return nil, err
		}
		client := xray.NewFromConfig(cfg)
		resp, err := client.GetServiceGraph(ctx, &xray.GetServiceGraphInput{
			StartTime: awsroot.Time(time.Now().UTC().Add(-6 * time.Hour)),
			EndTime:   awsroot.Time(time.Now().UTC()),
		})
		if err != nil {
			return nil, fmt.Errorf("xray GetServiceGraph (%s): %w", region, err)
		}
		for _, service := range resp.Services {
			if name := strings.TrimSpace(awsroot.ToString(service.Name)); name != "" {
				seen[name] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// QueryTraces returns X-Ray trace summaries.
func (r *AWSReader) QueryTraces(ctx context.Context, q TraceQuery) ([]TraceSummary, error) {
	from := q.From
	if from.IsZero() {
		from = time.Now().UTC().Add(-time.Hour)
	}
	to := q.To
	if to.IsZero() {
		to = time.Now().UTC()
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}

	out := make([]TraceSummary, 0)
	for _, region := range r.targetRegions() {
		cfg, err := r.loadConfig(ctx, region)
		if err != nil {
			return nil, err
		}
		client := xray.NewFromConfig(cfg)
		input := &xray.GetTraceSummariesInput{
			StartTime: awsroot.Time(from),
			EndTime:   awsroot.Time(to),
			Sampling:  awsroot.Bool(false),
		}
		if service := strings.TrimSpace(q.Service); service != "" {
			input.FilterExpression = awsroot.String(fmt.Sprintf("service(%q)", service))
		}
		resp, err := client.GetTraceSummaries(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("xray GetTraceSummaries (%s): %w", region, err)
		}
		for _, summary := range resp.TraceSummaries {
			traceID := awsroot.ToString(summary.Id)
			if traceID == "" {
				continue
			}
			status := "OK"
			if awsroot.ToBool(summary.HasFault) {
				status = "ERROR"
			} else if awsroot.ToBool(summary.HasError) {
				status = "ERROR"
			} else if awsroot.ToBool(summary.HasThrottle) {
				status = "THROTTLED"
			}
			startTime := awsroot.ToTime(summary.StartTime).UTC()
			duration := time.Duration(awsroot.ToFloat64(summary.Duration) * float64(time.Second))
			out = append(out, TraceSummary{
				TraceID:   traceID,
				Service:   strings.TrimSpace(q.Service),
				StartTime: startTime,
				EndTime:   startTime.Add(duration),
				Duration:  duration,
				Status:    status,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartTime.After(out[j].StartTime) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// GetTrace returns the spans of an X-Ray trace.
func (r *AWSReader) GetTrace(ctx context.Context, traceID string) ([]Span, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil, fmt.Errorf("trace id is required")
	}
	var lastErr error
	for _, region := range r.targetRegions() {
		cfg, err := r.loadConfig(ctx, region)
		if err != nil {
			return nil, err
		}
		client := xray.NewFromConfig(cfg)
		resp, err := client.BatchGetTraces(ctx, &xray.BatchGetTracesInput{TraceIds: []string{traceID}})
		if err != nil {
			lastErr = fmt.Errorf("xray BatchGetTraces (%s): %w", region, err)
			continue
		}
		for _, trace := range resp.Traces {
			spans := make([]Span, 0)
			for _, segment := range trace.Segments {
				document := awsroot.ToString(segment.Document)
				if document == "" {
					continue
				}
				spans = append(spans, parseXRayDocument(document, "", "")...)
			}
			if len(spans) > 0 {
				sort.Slice(spans, func(i, j int) bool { return spans[i].StartTime.Before(spans[j].StartTime) })
				return spans, nil
			}
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return []Span{}, nil
}

type xrayDocument struct {
	TraceID     string          `json:"trace_id"`
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	ParentID    string          `json:"parent_id"`
	StartTime   float64         `json:"start_time"`
	EndTime     float64         `json:"end_time"`
	Error       bool            `json:"error"`
	Fault       bool            `json:"fault"`
	Throttle    bool            `json:"throttle"`
	Namespace   string          `json:"namespace"`
	HTTP        json.RawMessage `json:"http"`
	AWS         json.RawMessage `json:"aws"`
	Metadata    map[string]any  `json:"metadata"`
	Annotations map[string]any  `json:"annotations"`
	Subsegments []xrayDocument  `json:"subsegments"`
}

func parseXRayDocument(document, parentService, parentID string) []Span {
	var doc xrayDocument
	if err := json.Unmarshal([]byte(document), &doc); err != nil {
		return nil
	}
	service := doc.Name
	if doc.Type == "subsegment" {
		if doc.Namespace != "" {
			service = doc.Namespace
		} else if parentService != "" {
			service = parentService
		}
	}
	parent := doc.ParentID
	if parent == "" {
		parent = parentID
	}

	attributes := make(map[string]string)
	for key, value := range doc.Metadata {
		attributes["metadata."+key] = stringifyXRayValue(value)
	}
	for key, value := range doc.Annotations {
		attributes["annotation."+key] = stringifyXRayValue(value)
	}
	if doc.Namespace != "" {
		attributes["aws.namespace"] = doc.Namespace
	}
	if doc.Error {
		attributes["error"] = "true"
	}
	if doc.Fault {
		attributes["fault"] = "true"
	}
	if doc.Throttle {
		attributes["throttle"] = "true"
	}
	statusCode := ""
	if doc.Error || doc.Fault {
		statusCode = "ERROR"
	}
	if httpStatus := xrayHTTPStatus(doc.HTTP); httpStatus != "" {
		attributes["http.status_code"] = httpStatus
		if strings.HasPrefix(httpStatus, "5") {
			statusCode = "ERROR"
		}
	}

	span := Span{
		TraceID:      doc.TraceID,
		SpanID:       doc.ID,
		ParentSpanID: parent,
		Name:         doc.Name,
		Service:      service,
		StartTime:    secondsToTime(doc.StartTime),
		EndTime:      secondsToTime(doc.EndTime),
		StatusCode:   statusCode,
		Attributes:   attributes,
	}

	out := []Span{span}
	for _, child := range doc.Subsegments {
		childService := service
		out = append(out, parseXRayDocument(mustJSON(child), childService, doc.ID)...)
	}
	return out
}

func xrayHTTPStatus(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload struct {
		Response struct {
			Status any `json:"status"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return stringifyXRayValue(payload.Response.Status)
}

func stringifyXRayValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	default:
		if encoded, err := json.Marshal(typed); err == nil {
			return string(encoded)
		}
		return fmt.Sprintf("%v", typed)
	}
}

func secondsToTime(seconds float64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	whole := int64(seconds)
	nanos := int64((seconds - float64(whole)) * float64(time.Second))
	return time.Unix(whole, nanos).UTC()
}

func mustJSON(v any) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(encoded)
}
