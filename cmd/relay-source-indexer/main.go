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
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, indexerprotocol.MaxRequestBytes+1))
	if len(raw) > indexerprotocol.MaxRequestBytes {
		err = errors.New("request too large")
	}
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
			response.Failure = &indexerprotocol.BuildFailure{Code: indexerprotocol.FailureCancelled, Message: "build cancelled"}
		}
	}
	if response.Status == indexerprotocol.BuildStatusFailed && response.Failure == nil {
		code, message := indexerprotocol.FailureInvalidRequest, "invalid build request"
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
	n, writeErr := os.Stdout.Write(b)
	if writeErr != nil || n != len(b) {
		os.Exit(1)
	}
	if response.Status != indexerprotocol.BuildStatusSuccess {
		os.Exit(1)
	}
}
