package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/notify"
)

// notifyTestCommand sends a test alert to a webhook channel.
func notifyTestCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("notify test", flag.ContinueOnError)
	url := flags.String("url", "", "webhook URL")
	secret := flags.String("secret", "", "HMAC signing secret")
	secretEnv := flags.String("secret-env", "", "environment variable holding the HMAC signing secret")
	partition := flags.String("partition", "test", "partition name to include in the test alert")
	return &command.Command{Description: "Send a test partition alert to a webhook", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*url) == "" {
			return fmt.Errorf("--url is required")
		}
		signingSecret := strings.TrimSpace(*secret)
		if signingSecret == "" && strings.TrimSpace(*secretEnv) != "" {
			signingSecret = strings.TrimSpace(os.Getenv(*secretEnv))
		}
		notifier := notify.NewWebhookNotifier("test", *url, signingSecret, 10*time.Second)
		alert := notify.Alert{
			Event:     "partition.failing",
			Partition: strings.TrimSpace(*partition),
			Status:    "firing",
			Severity:  notify.SeverityWarning,
			Summary:   "Guardian notification test alert",
			Details:   map[string]string{"test": "true"},
			FiredAt:   time.Now().UTC(),
		}
		if err := notifier.Send(ctx, alert); err != nil {
			return err
		}
		printer.PrintText("test alert delivered to %s\n", *url)
		return nil
	}}
}
