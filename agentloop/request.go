package agentloop

import (
	"context"
	"net/http/httptrace"
	"time"
)

const DefaultRequestTimeout = 10 * time.Minute

func traceModelRequest(ctx context.Context, cb Callbacks) context.Context {
	if cb.OnStatus == nil {
		return ctx
	}
	started := time.Now()
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				cb.status("debug", "HTTP-запрос отправлен; ожидание ответа провайдера")
			}
		},
		GotFirstResponseByte: func() {
			cb.status("debug", "Начало HTTP-ответа получено через "+time.Since(started).Round(time.Millisecond).String()+"; ожидание полного ответа")
		},
	})
}
