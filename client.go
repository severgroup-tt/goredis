package apm

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-redis/redis/v8"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
)

type opentracingHook struct{ tracer opentracing.Tracer }

var _ redis.Hook = opentracingHook{}

func (h opentracingHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	spanName := strings.ToUpper(cmd.Name())
	span, _ := opentracing.StartSpanFromContextWithTracer(ctx, h.tracer, spanName)

	ext.DBType.Set(span, "redis")
	ext.SpanKindRPCClient.Set(span)
	ext.DBStatement.Set(span, fmt.Sprintf("%v", cmd.Args()))
	// to maintain compatibility with opentelemetry convention
	span.SetTag("db.system", "redis")

	ctx = opentracing.ContextWithSpan(ctx, span)

	return ctx, nil
}

func (h opentracingHook) AfterProcess(ctx context.Context, cmd redis.Cmder) error {
	span := opentracing.SpanFromContext(ctx)
	span.Finish()
	return nil
}

func (h opentracingHook) BeforeProcessPipeline(ctx context.Context, cmds []redis.Cmder) (context.Context, error) {
	span, ctx := opentracing.StartSpanFromContextWithTracer(ctx, h.tracer, "(pipeline)")
	dbMethod := formatCommandsAsDbMethods(cmds)

	ext.DBType.Set(span, "redis")
	ext.SpanKindRPCClient.Set(span)
	ext.DBStatement.Set(span, fmt.Sprintf("%v", dbMethod))
	// to maintain compatibility with opentelemetry convention
	span.SetTag("db.system", "redis")

	ctx = opentracing.ContextWithSpan(ctx, span)
	return ctx, nil
}

func (h opentracingHook) AfterProcessPipeline(ctx context.Context, cmds []redis.Cmder) error {
	span := opentracing.SpanFromContext(ctx)
	span.Finish()
	return nil
}

// Client is the interface returned by Wrap.
//
// Client implements redis.UniversalClient
type Client interface {
	redis.UniversalClient

	// ClusterClient returns the wrapped *redis.ClusterClient,
	// or nil if a non-cluster client is wrapped.
	Cluster() *redis.ClusterClient

	// Ring returns the wrapped *redis.Ring,
	// or nil if a non-ring client is wrapped.
	RingClient() *redis.Ring

	// WithContext returns a shallow copy of the client with
	// its context changed to ctx and will add instrumentation
	// with client.WrapProcess and client.WrapProcessPipeline
	//
	// To report commands as spans, ctx must contain a transaction or span.
	WithContext(ctx context.Context) Client
}

// Wrap wraps client such that executed commands are reported as spans to Elastic APM,
// using the client's associated context.
// A context-specific client may be obtained by using Client.WithContext.
func Wrap(client redis.UniversalClient, tracer opentracing.Tracer) Client {
	if tracer == nil {
		tracer = opentracing.GlobalTracer()
	}

	switch client.(type) {
	case *redis.Client:
		return contextClient{Client: client.(*redis.Client), tracer: tracer}
	case *redis.ClusterClient:
		return contextClusterClient{ClusterClient: client.(*redis.ClusterClient), tracer: tracer}
	case *redis.Ring:
		return contextRingClient{Ring: client.(*redis.Ring), tracer: tracer}
	}

	return client.(Client)
}

type contextClient struct {
	*redis.Client
	tracer opentracing.Tracer
}

func (c contextClient) WithContext(ctx context.Context) Client {
	c.Client = c.Client.WithContext(ctx)

	c.AddHook(opentracingHook{c.tracer})

	return c
}

func (c contextClient) Cluster() *redis.ClusterClient {
	return nil
}

func (c contextClient) RingClient() *redis.Ring {
	return nil
}

type contextClusterClient struct {
	*redis.ClusterClient
	tracer opentracing.Tracer
}

func (c contextClusterClient) Cluster() *redis.ClusterClient {
	return c.ClusterClient
}

func (c contextClusterClient) RingClient() *redis.Ring {
	return nil
}

func (c contextClusterClient) WithContext(ctx context.Context) Client {
	c.ClusterClient = c.ClusterClient.WithContext(ctx)

	c.AddHook(opentracingHook{c.tracer})

	return c
}

type contextRingClient struct {
	*redis.Ring
	tracer opentracing.Tracer
}

func (c contextRingClient) Cluster() *redis.ClusterClient {

	return nil
}

func (c contextRingClient) RingClient() *redis.Ring {
	return c.Ring
}

func (c contextRingClient) WithContext(ctx context.Context) Client {
	c.Ring = c.Ring.WithContext(ctx)

	c.AddHook(opentracingHook{c.tracer})

	return c
}

func formatCommandsAsDbMethods(cmds []redis.Cmder) string {
	cmdsAsDbMethods := make([]string, len(cmds))
	for i, cmd := range cmds {
		dbMethod := cmd.Name()
		cmdsAsDbMethods[i] = dbMethod
	}
	return strings.Join(cmdsAsDbMethods, " -> ")
}
