package inference

import (
	"context"
	"errors"
	"reflect"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const inferenceInstrumentationName = "powercontext.inference"

// TraceTextModel instruments one physical provider request. The span contract
// deliberately contains no prompt, message, output, model request parameters,
// credentials, or provider response bodies.
func TraceTextModel(model TextModel, provider trace.TracerProvider) TextModel {
	if model == nil {
		return nil
	}
	if provider == nil {
		provider = otel.GetTracerProvider()
	}
	return tracedTextModel{model: model, tracer: provider.Tracer(inferenceInstrumentationName)}
}

type tracedTextModel struct {
	model  TextModel
	tracer trace.Tracer
}

func (m tracedTextModel) Complete(ctx context.Context, request TextRequest) (TextResponse, error) {
	ctx, span := startInferenceSpan(ctx, m.tracer, "generate")
	response, err := m.model.Complete(ctx, request)
	finishInferenceSpan(ctx, span, err)
	return response, err
}

// TraceEmbeddingTransport instruments each provider batch while leaving the
// batching, validation, normalization, and total timeout under
// BatchedEmbeddingModel's ownership.
func TraceEmbeddingTransport(transport EmbeddingTransport, provider trace.TracerProvider) EmbeddingTransport {
	if transport == nil {
		return nil
	}
	if provider == nil {
		provider = otel.GetTracerProvider()
	}
	return tracedEmbeddingTransport{transport: transport, tracer: provider.Tracer(inferenceInstrumentationName)}
}

type tracedEmbeddingTransport struct {
	transport EmbeddingTransport
	tracer    trace.Tracer
}

func (t tracedEmbeddingTransport) Embed(
	ctx context.Context,
	inputs []string,
	inputType EmbeddingInputType,
) (ProviderEmbeddingResult, error) {
	ctx, span := startInferenceSpan(ctx, t.tracer, "embed")
	result, err := t.transport.Embed(ctx, inputs, inputType)
	finishInferenceSpan(ctx, span, err)
	return result, err
}

func startInferenceSpan(ctx context.Context, tracer trace.Tracer, operation string) (context.Context, trace.Span) {
	return tracer.Start(
		ctx,
		"powercontext inference."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("powercontext.operation.name", operation),
			attribute.String("powercontext.operation.unit", "inference"),
		),
	)
}

func finishInferenceSpan(ctx context.Context, span trace.Span, err error) {
	outcome := "success"
	if err != nil {
		outcome = "failure"
		span.SetAttributes(attribute.String("error.type", reflect.TypeOf(err).String()))
		span.SetStatus(codes.Error, "")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		outcome = "cancelled"
	}
	span.SetAttributes(attribute.String("powercontext.operation.outcome", outcome))
	span.End()
}

var (
	_ TextModel          = tracedTextModel{}
	_ EmbeddingTransport = tracedEmbeddingTransport{}
)
