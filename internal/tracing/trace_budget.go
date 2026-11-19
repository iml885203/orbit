package tracing

import "strings"

const (
	maxTraceBytes         = 64 << 20
	maxTraceIdentityBytes = 1024
	spanOverhead          = 256
	traceOverhead         = 128
)

func retainedSpan(sp Span) (Span, bool) {
	for _, id := range []string{sp.TraceID, sp.SpanID, sp.ParentID, sp.Service} {
		if len(id) > maxTraceIdentityBytes {
			return Span{}, false
		}
	}
	sp.TraceID = strings.Clone(sp.TraceID)
	sp.SpanID = strings.Clone(sp.SpanID)
	sp.ParentID = strings.Clone(sp.ParentID)
	sp.Service = strings.Clone(sp.Service)
	sp.Name = strings.Clone(truncateLogValue(sp.Name))
	sp.StatusMsg = strings.Clone(truncateLogValue(sp.StatusMsg))
	sp.Kind = strings.Clone(truncateLogValue(sp.Kind))
	sp.Status = strings.Clone(truncateLogValue(sp.Status))
	if attrBytes(sp.Attributes) > maxAttrBytesPerSpan || len(sp.Attributes) > maxLogAttributes {
		sp.Attributes = nil
	} else if sp.Attributes != nil {
		attrs := make(map[string]string, len(sp.Attributes))
		for k, v := range sp.Attributes {
			attrs[strings.Clone(k)] = strings.Clone(v)
		}
		sp.Attributes = attrs
	}
	return sp, true
}

func retainedSpanBytes(sp Span) int {
	return spanOverhead + len(sp.TraceID) + len(sp.SpanID) + len(sp.ParentID) + len(sp.Service) +
		len(sp.Name) + len(sp.StatusMsg) + len(sp.Kind) + len(sp.Status) +
		len(sp.Attributes)*logAttributeOverhead + attrBytes(sp.Attributes)
}
