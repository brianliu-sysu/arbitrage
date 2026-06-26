// Package tracing 提供 OpenTelemetry 链路追踪初始化。
//
// 仅在配置了 tracing_endpoint 时启用，导出到远程 OTLP collector（Jaeger、Tempo 等）。
// 未配置时不产生任何 trace 输出。
package tracing

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config 链路追踪配置。
type Config struct {
	Endpoint    string // OTLP HTTP 导出端点，空字符串表示禁用
	ServiceName string // 服务名称，默认 "arbitrage"
}

// ShutdownFunc 关闭 TracerProvider，确保所有 span 被导出。
type ShutdownFunc func(context.Context) error

// Init 初始化 OpenTelemetry TracerProvider。
// endpoint 为空时追踪被禁用（零开销）。
func Init(cfg Config) (ShutdownFunc, error) {
	if cfg.Endpoint == "" {
		return func(ctx context.Context) error { return nil }, nil
	}

	if cfg.ServiceName == "" {
		cfg.ServiceName = "arbitrage"
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	exporter, err := otlptracehttp.New(context.Background(),
		otlptracehttp.WithEndpoint(cfg.Endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	bsp := sdktrace.NewBatchSpanProcessor(exporter,
		sdktrace.WithBatchTimeout(5*time.Second),
		sdktrace.WithMaxExportBatchSize(512),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)

	otel.SetTracerProvider(tp)

	log.Printf("[tracing] initialized, exporting to OTLP endpoint: %s", cfg.Endpoint)

	return func(ctx context.Context) error {
		log.Println("[tracing] shutting down...")
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := tp.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown tracer provider: %w", err)
		}
		return nil
	}, nil
}

// Tracer 返回全局 Tracer。
func Tracer() trace.Tracer {
	return otel.Tracer("arbitrage")
}
