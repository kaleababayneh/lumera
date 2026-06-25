package keeper

import (
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/hashicorp/go-metrics"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/x/router/types"
)

func telemetryNow() time.Time {
	return telemetry.Now()
}

func telemetryLabelsForTool(toolID string) []metrics.Label {
	toolID = strings.TrimSpace(toolID)
	if toolID == "" {
		return nil
	}
	return []metrics.Label{telemetry.NewLabel("tool_id", toolID)}
}

func telemetryAppendLabel(labels []metrics.Label, key, value string) []metrics.Label {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return labels
	}
	return append(labels, telemetry.NewLabel(key, value))
}

func telemetryCodeFromError(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	if st, ok := status.FromError(err); ok {
		return st.Code()
	}
	return codes.Unknown
}

func telemetryRecordGRPC(method string, code codes.Code, start time.Time, labels []metrics.Label) {
	if !start.IsZero() {
		telemetry.ModuleMeasureSince(types.ModuleName, start, "grpc", method)
	}

	labelsWithCode := append(make([]metrics.Label, 0, len(labels)+1), labels...)
	labelsWithCode = append(labelsWithCode, telemetry.NewLabel("code", code.String()))

	telemetry.IncrCounterWithLabels([]string{types.ModuleName, "grpc", method, "requests_total"}, 1, labelsWithCode)

	if code == codes.OK {
		telemetry.IncrCounterWithLabels([]string{types.ModuleName, "grpc", method, "success_total"}, 1, labelsWithCode)
	} else {
		telemetry.IncrCounterWithLabels([]string{types.ModuleName, "grpc", method, "error_total"}, 1, labelsWithCode)
	}
}
