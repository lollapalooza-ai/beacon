package cloud

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/sony/gobreaker"
	"golang.org/x/time/rate"
)

// ResilientOpts holds configuration for the circuit breaker and rate limiter.
type ResilientOpts struct {
	MaxRequests uint32
	Interval    time.Duration
	Timeout     time.Duration
	RateLimit   float64
	RateBurst   int
}

// DefaultResilientOpts provides sensible defaults for resiliency.
func DefaultResilientOpts() ResilientOpts {
	return ResilientOpts{
		MaxRequests: 5,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		RateLimit:   10.0,
		RateBurst:   20,
	}
}

// ResilientAdapter wraps an Adapter with rate limiting and circuit breaking.
type ResilientAdapter struct {
	inner   Adapter
	breaker *gobreaker.CircuitBreaker
	limiter *rate.Limiter
}

// NewResilientAdapter creates a new resilient wrapper around the given cloud adapter.
func NewResilientAdapter(inner Adapter, opts ResilientOpts) *ResilientAdapter {
	st := gobreaker.Settings{
		Name:        inner.Name() + "-breaker",
		MaxRequests: opts.MaxRequests,
		Interval:    opts.Interval,
		Timeout:     opts.Timeout,
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Warn().
				Str("breaker_name", name).
				Str("from_state", from.String()).
				Str("to_state", to.String()).
				Msg("circuit breaker state changed")
		},
	}

	return &ResilientAdapter{
		inner:   inner,
		breaker: gobreaker.NewCircuitBreaker(st),
		limiter: rate.NewLimiter(rate.Limit(opts.RateLimit), opts.RateBurst),
	}
}

// Name returns the underlying adapter name.
func (r *ResilientAdapter) Name() string {
	return r.inner.Name()
}

// QuerySpotPrices retrieves current spot instance pricing with resiliency.
func (r *ResilientAdapter) QuerySpotPrices(ctx context.Context, req *SpotPriceRequest) ([]SpotPrice, error) {
	if err := r.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	res, err := r.breaker.Execute(func() (interface{}, error) {
		return r.inner.QuerySpotPrices(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	return res.([]SpotPrice), nil
}

// ProvisionInstance launches a new spot instance with resiliency.
func (r *ResilientAdapter) ProvisionInstance(ctx context.Context, req *ProvisionRequest) (*Instance, error) {
	if err := r.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	res, err := r.breaker.Execute(func() (interface{}, error) {
		return r.inner.ProvisionInstance(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	return res.(*Instance), nil
}

// GetInstanceStatus retrieves the current status of an instance with resiliency.
func (r *ResilientAdapter) GetInstanceStatus(ctx context.Context, instanceID string, region string) (*InstanceStatus, error) {
	if err := r.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	res, err := r.breaker.Execute(func() (interface{}, error) {
		return r.inner.GetInstanceStatus(ctx, instanceID, region)
	})
	if err != nil {
		return nil, err
	}

	return res.(*InstanceStatus), nil
}

// TerminateInstance terminates a running instance with resiliency.
func (r *ResilientAdapter) TerminateInstance(ctx context.Context, instanceID string, region string) error {
	if err := r.limiter.Wait(ctx); err != nil {
		return err
	}

	_, err := r.breaker.Execute(func() (interface{}, error) {
		return nil, r.inner.TerminateInstance(ctx, instanceID, region)
	})

	return err
}
