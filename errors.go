package sparkle

import "fmt"

// HTTPError is a non-OK status from the feed or download endpoint (other than
// the 401/403 that map to ErrUnauthorized).
type HTTPError struct {
	Op     string
	Status int
}

func (e *HTTPError) Error() string { return fmt.Sprintf("%s: HTTP %d", e.Op, e.Status) }

// SizeMismatchError is returned when a downloaded artifact's length does not
// match the appcast enclosure's declared length.
type SizeMismatchError struct {
	Got, Want int64
}

func (e *SizeMismatchError) Error() string {
	return fmt.Sprintf("download size mismatch: got %d bytes, expected %d", e.Got, e.Want)
}
