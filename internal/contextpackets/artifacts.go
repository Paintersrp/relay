package contextpackets

import (
	"encoding/json"
	"time"

	"relay/internal/artifacts"
)

func writeArtifacts(generatedAt string, taskSlug string, packet ContextPacket, coverage ContextCoverageReport) (packetJSONPath, packetMarkdownPath, coverageReportPath string, err error) {
	date := generatedAt
	if len(date) >= len("2006-01-02") {
		date = date[:len("2006-01-02")]
	} else {
		date = time.Now().UTC().Format("2006-01-02")
	}

	packetJSON, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return "", "", "", err
	}
	packetJSONPath, err = artifacts.WriteContext(date, taskSlug, "context_packet_json", append(packetJSON, '\n'))
	if err != nil {
		return "", "", "", err
	}

	packetMarkdown := []byte(renderMarkdown(packet))
	packetMarkdownPath, err = artifacts.WriteContext(date, taskSlug, "context_packet_markdown", packetMarkdown)
	if err != nil {
		return "", "", "", err
	}

	coverageJSON, err := json.MarshalIndent(coverage, "", "  ")
	if err != nil {
		return "", "", "", err
	}
	coverageReportPath, err = artifacts.WriteContext(date, taskSlug, "context_coverage_report_json", append(coverageJSON, '\n'))
	if err != nil {
		return "", "", "", err
	}
	return packetJSONPath, packetMarkdownPath, coverageReportPath, nil
}
