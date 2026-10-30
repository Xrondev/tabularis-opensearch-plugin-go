package opensearch

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
)

func httpFailure(status int) error {
	switch {
	case status == 401:
		return fmt.Errorf("OpenSearch authentication failed (HTTP status %d); check credentials", status)
	case status == 403:
		return fmt.Errorf("OpenSearch permission denied (HTTP status %d); check access policies", status)
	case status == 404:
		return fmt.Errorf("OpenSearch resource or API not found (HTTP status %d); check the target and API availability", status)
	case status == 429:
		return fmt.Errorf("OpenSearch rate limit exceeded (HTTP status %d); retry later", status)
	case status >= 500 && status <= 599:
		return fmt.Errorf("OpenSearch service error (HTTP status %d); check service health", status)
	default:
		return fmt.Errorf("OpenSearch request rejected (HTTP status %d)", status)
	}
}

func transportFailure(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("OpenSearch request timed out: %w", context.DeadlineExceeded)
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("OpenSearch request canceled: %w", context.Canceled)
	}
	var verify *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &verify) || errors.As(err, &authority) || errors.As(err, &hostname) || errors.As(err, &invalid) {
		return errors.New("OpenSearch TLS certificate verification failed; check the endpoint and CA certificate")
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return fmt.Errorf("OpenSearch request timed out: %w", context.DeadlineExceeded)
	}
	return errors.New("OpenSearch transport failed; check network connectivity and credentials")
}
