package worker

import (
	"go.opentelemetry.io/otel"
)

// tracer picks up the global TracerProvider installed by tracing.Init.
// Emitting spans through this instance guarantees they are children of the
// span extracted from the incoming AMQP message headers.
var tracer = otel.Tracer("avatars-service/internal/worker")
