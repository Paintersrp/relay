// relay-source-indexer builds one source-index staging generation from stdin.
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"

	"relay/internal/sourceindex/indexer"
	"relay/internal/sourceindex/indexerprotocol"
)

func main() {
	raw, err := io.ReadAll(os.Stdin)
	request, parseErr := indexerprotocol.ParseBuildRequest(raw)
	if err == nil {
		err = parseErr
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	response := indexerprotocol.BuildResponse{Version: indexerprotocol.ProtocolVersion, Status: indexerprotocol.BuildStatusFailed}
	if request.GenerationID != "" {
		response.GenerationID = request.GenerationID
	}
	if err == nil {
		var result indexerprotocol.BuildResult
		result, err = indexer.Build(ctx, request)
		if err == nil {
			response.Status = indexerprotocol.BuildStatusSuccess
			response.Result = &result
		} else if ctx.Err() != nil {
			response.Failure = &indexerprotocol.BuildFailure{Code: "cancelled", Message: "build cancelled"}
		}
	}
	if response.Status == indexerprotocol.BuildStatusFailed && response.Failure == nil {
		code, message := "invalid_request", "invalid build request"
		var f *indexer.Failure
		if errors.As(err, &f) {
			code, message = f.Code, f.Message
		}
		response.Failure = &indexerprotocol.BuildFailure{Code: code, Message: message}
	}
	b, marshalErr := indexerprotocol.MarshalBuildResponse(response)
	if marshalErr != nil {
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(b)
	if response.Status != indexerprotocol.BuildStatusSuccess {
		os.Exit(1)
	}
}
