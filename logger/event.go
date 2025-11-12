package logger

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// Event wraps zerolog.Event to intercept Err() for stack trace injection.
type Event struct {
	*zerolog.Event
}

// Err wraps the error with stack trace if needed before logging.
func (e *Event) Err(err error) *Event {
	if err == nil {
		return e
	}
	err = ensureStack(err, 1)
	e.Event = e.Event.Err(err)
	return e
}

// Ctx adds context to the event.
func (e *Event) Ctx(ctx context.Context) *Event {
	e.Event = e.Event.Ctx(ctx)
	return e
}

// Str adds a string field.
func (e *Event) Str(key, val string) *Event {
	e.Event = e.Event.Str(key, val)
	return e
}

// Strs adds a string array field.
func (e *Event) Strs(key string, vals []string) *Event {
	e.Event = e.Event.Strs(key, vals)
	return e
}

// Int adds an int field.
func (e *Event) Int(key string, val int) *Event {
	e.Event = e.Event.Int(key, val)
	return e
}

// Int8 adds an int8 field.
func (e *Event) Int8(key string, val int8) *Event {
	e.Event = e.Event.Int8(key, val)
	return e
}

// Int16 adds an int16 field.
func (e *Event) Int16(key string, val int16) *Event {
	e.Event = e.Event.Int16(key, val)
	return e
}

// Int32 adds an int32 field.
func (e *Event) Int32(key string, val int32) *Event {
	e.Event = e.Event.Int32(key, val)
	return e
}

// Int64 adds an int64 field.
func (e *Event) Int64(key string, val int64) *Event {
	e.Event = e.Event.Int64(key, val)
	return e
}

// Ints adds an int array field.
func (e *Event) Ints(key string, vals []int) *Event {
	e.Event = e.Event.Ints(key, vals)
	return e
}

// Uint adds a uint field.
func (e *Event) Uint(key string, val uint) *Event {
	e.Event = e.Event.Uint(key, val)
	return e
}

// Uint8 adds a uint8 field.
func (e *Event) Uint8(key string, val uint8) *Event {
	e.Event = e.Event.Uint8(key, val)
	return e
}

// Uint16 adds a uint16 field.
func (e *Event) Uint16(key string, val uint16) *Event {
	e.Event = e.Event.Uint16(key, val)
	return e
}

// Uint32 adds a uint32 field.
func (e *Event) Uint32(key string, val uint32) *Event {
	e.Event = e.Event.Uint32(key, val)
	return e
}

// Uint64 adds a uint64 field.
func (e *Event) Uint64(key string, val uint64) *Event {
	e.Event = e.Event.Uint64(key, val)
	return e
}

// Uints adds a uint array field.
func (e *Event) Uints(key string, vals []uint) *Event {
	e.Event = e.Event.Uints(key, vals)
	return e
}

// Bool adds a bool field.
func (e *Event) Bool(key string, val bool) *Event {
	e.Event = e.Event.Bool(key, val)
	return e
}

// Bools adds a bool array field.
func (e *Event) Bools(key string, vals []bool) *Event {
	e.Event = e.Event.Bools(key, vals)
	return e
}

// Float32 adds a float32 field.
func (e *Event) Float32(key string, val float32) *Event {
	e.Event = e.Event.Float32(key, val)
	return e
}

// Float64 adds a float64 field.
func (e *Event) Float64(key string, val float64) *Event {
	e.Event = e.Event.Float64(key, val)
	return e
}

// Floats32 adds a float32 array field.
func (e *Event) Floats32(key string, vals []float32) *Event {
	e.Event = e.Event.Floats32(key, vals)
	return e
}

// Floats64 adds a float64 array field.
func (e *Event) Floats64(key string, vals []float64) *Event {
	e.Event = e.Event.Floats64(key, vals)
	return e
}

// Dur adds a duration field.
func (e *Event) Dur(key string, val time.Duration) *Event {
	e.Event = e.Event.Dur(key, val)
	return e
}

// Durs adds a duration array field.
func (e *Event) Durs(key string, vals []time.Duration) *Event {
	e.Event = e.Event.Durs(key, vals)
	return e
}

// Time adds a time field.
func (e *Event) Time(key string, val time.Time) *Event {
	e.Event = e.Event.Time(key, val)
	return e
}

// Times adds a time array field.
func (e *Event) Times(key string, vals []time.Time) *Event {
	e.Event = e.Event.Times(key, vals)
	return e
}

// TimeDiff adds a duration field representing the difference between two times.
func (e *Event) TimeDiff(key string, t time.Time, start time.Time) *Event {
	e.Event = e.Event.TimeDiff(key, t, start)
	return e
}

// Timestamp adds the current timestamp.
func (e *Event) Timestamp() *Event {
	e.Event = e.Event.Timestamp()
	return e
}

// Any adds an interface{} field.
func (e *Event) Any(key string, val interface{}) *Event {
	e.Event = e.Event.Interface(key, val)
	return e
}

// Interface adds an interface{} field.
func (e *Event) Interface(key string, val interface{}) *Event {
	e.Event = e.Event.Interface(key, val)
	return e
}

// Bytes adds a bytes field.
func (e *Event) Bytes(key string, val []byte) *Event {
	e.Event = e.Event.Bytes(key, val)
	return e
}

// Hex adds a hex-encoded bytes field.
func (e *Event) Hex(key string, val []byte) *Event {
	e.Event = e.Event.Hex(key, val)
	return e
}

// AnErr adds an error field with a custom key.
func (e *Event) AnErr(key string, err error) *Event {
	if err == nil {
		return e
	}
	err = ensureStack(err, 1)
	e.Event = e.Event.AnErr(key, err)
	return e
}

// Errs adds a slice of errors field.
func (e *Event) Errs(key string, errs []error) *Event {
	wrappedErrs := make([]error, len(errs))
	for i, err := range errs {
		if err == nil {
			continue
		}
		wrappedErrs[i] = ensureStack(err, 1)
	}
	e.Event = e.Event.Errs(key, wrappedErrs)
	return e
}

// Object marshals an object.
func (e *Event) Object(key string, obj zerolog.LogObjectMarshaler) *Event {
	e.Event = e.Event.Object(key, obj)
	return e
}

// EmbedObject embeds an object.
func (e *Event) EmbedObject(obj zerolog.LogObjectMarshaler) *Event {
	e.Event = e.Event.EmbedObject(obj)
	return e
}

// Array adds an array field.
func (e *Event) Array(key string, arr zerolog.LogArrayMarshaler) *Event {
	e.Event = e.Event.Array(key, arr)
	return e
}

// Dict adds a sub-dictionary.
func (e *Event) Dict(key string, dict *zerolog.Event) *Event {
	e.Event = e.Event.Dict(key, dict)
	return e
}

// RawJSON adds a pre-encoded JSON field.
func (e *Event) RawJSON(key string, b []byte) *Event {
	e.Event = e.Event.RawJSON(key, b)
	return e
}

// Caller adds the file:line of the caller.
func (e *Event) Caller(skip ...int) *Event {
	e.Event = e.Event.Caller(skip...)
	return e
}

// Stack enables stack trace printing.
func (e *Event) Stack() *Event {
	e.Event = e.Event.Stack()
	return e
}

// Enabled returns whether the event is enabled.
func (e *Event) Enabled() bool {
	return e.Event.Enabled()
}

// Discard disables the event.
func (e *Event) Discard() *Event {
	e.Event = e.Event.Discard()
	return e
}
