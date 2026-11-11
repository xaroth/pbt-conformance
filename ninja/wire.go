package ninja

import (
	"encoding/binary"
	"errors"
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
)

// ErrFormat marks a response the client cannot read.
var ErrFormat = errors.New("unreadable response")

// field is one protobuf field: its number and either its varint or its bytes.
type field struct {
	num   protowire.Number
	value uint64
	bytes []byte
}

// fields splits a protobuf message into its fields, in order.
func fields(b []byte) ([]field, error) {
	var out []field
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, fmt.Errorf("%w: %w", ErrFormat, protowire.ParseError(n))
		}
		b = b[n:]
		f := field{num: num}
		switch typ {
		case protowire.VarintType:
			f.value, n = protowire.ConsumeVarint(b)
		case protowire.BytesType:
			f.bytes, n = protowire.ConsumeBytes(b)
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
		}
		if n < 0 {
			return nil, fmt.Errorf("%w: %w", ErrFormat, protowire.ParseError(n))
		}
		b = b[n:]
		out = append(out, f)
	}
	return out, nil
}

// varints reads a packed list of varints.
func varints(b []byte) ([]uint64, error) {
	var out []uint64
	for len(b) > 0 {
		v, n := protowire.ConsumeVarint(b)
		if n < 0 {
			return nil, fmt.Errorf("%w: %w", ErrFormat, protowire.ParseError(n))
		}
		out = append(out, v)
		b = b[n:]
	}
	return out, nil
}

// NDIC dictionary layout: magic, version, flags, entry count, an 8-byte hash, the sampling
// stride and the number of samples, the size of the length list, the samples (offset and index,
// 4 bytes each), the entries' lengths as varints, then the entries' text.
const (
	ndicMagic   = "NDIC"
	ndicCount   = 12
	ndicSamples = 28
	ndicLengths = 32
	ndicHeader  = 36
	ndicSample  = 8
)

// dictionary reads a poe.ninja dictionary: the names that a search's indexes refer to.
func dictionary(b []byte) ([]string, error) {
	if len(b) < ndicHeader || string(b[:4]) != ndicMagic {
		return nil, fmt.Errorf("%w: not a dictionary", ErrFormat)
	}
	count := int(binary.LittleEndian.Uint32(b[ndicCount:]))
	start := ndicHeader + int(binary.LittleEndian.Uint32(b[ndicSamples:]))*ndicSample
	end := start + int(binary.LittleEndian.Uint32(b[ndicLengths:]))
	if end > len(b) || start > end {
		return nil, fmt.Errorf("%w: dictionary lengths", ErrFormat)
	}
	lengths, err := varints(b[start:end])
	if err != nil {
		return nil, err
	}
	text := b[end:]
	names := make([]string, 0, len(lengths))
	for _, n := range lengths {
		if n > uint64(len(text)) {
			return nil, fmt.Errorf("%w: dictionary text", ErrFormat)
		}
		names = append(names, string(text[:n]))
		text = text[n:]
	}
	if len(names) != count || len(text) != 0 {
		return nil, fmt.Errorf("%w: dictionary of %d entries holds %d", ErrFormat, count, len(names))
	}
	return names, nil
}
