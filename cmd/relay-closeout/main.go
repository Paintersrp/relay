package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"relay/internal/closeout"
)

func main() {
	var messageFlag string
	var slugFlag string
	var dryRun bool

	flag.StringVar(&messageFlag, "message", "", "commit message")
	flag.StringVar(&slugFlag, "slug", "", "closeout artifact slug")
	flag.BoolVar(&dryRun, "dry-run", false, "skip commit and push")
	flag.Parse()

	message := messageFlag
	if message == "" {
		message = os.Getenv("MESSAGE")
	}
	slug := slugFlag
	if slug == "" {
		slug = os.Getenv("SLUG")
	}
	if message == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --message or MESSAGE is required")
		os.Exit(2)
	}

	report, err := closeout.Run(context.Background(), closeout.Options{
		Message: message,
		Slug:    slug,
		DryRun:  dryRun,
	})

	fmt.Printf("validation: %s", report.Validation.Status)
	if report.Validation.Command != "" {
		fmt.Printf(" (%s)", report.Validation.Command)
	}
	fmt.Println()
	fmt.Printf("evidence_json: %s\n", report.EvidencePaths.JSON)
	fmt.Printf("evidence_markdown: %s\n", report.EvidencePaths.Markdown)
	fmt.Printf("commit: %s\n", report.CommitStatus)
	fmt.Printf("push: %s\n", report.PushStatus)
	if report.MechanicalBlocker != nil {
		fmt.Printf("mechanical_blocker: %s\n", report.MechanicalBlocker.Stage)
	}

	if err != nil {
		os.Exit(1)
	}
}
