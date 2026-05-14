package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// data atom type codes used by iTunes ilst entries.
const (
	DataTypeBinary uint32 = 0
	DataTypeUTF8   uint32 = 1
	DataTypeUTF16  uint32 = 2
	DataTypeJPEG   uint32 = 13
	DataTypePNG    uint32 = 14
	DataTypeBEInt  uint32 = 21
)

// DataAtom is the body of a "data" child box found inside ilst
// entries. Layout: 4-byte type code | 4-byte locale (always 0) |
// payload bytes.
type DataAtom struct {
	TypeCode uint32
	Locale   uint32
	Payload  []byte
}

// parseDataAtom decodes the body of a "data" box (header already
// consumed by the caller).
func parseDataAtom(body []byte) (*DataAtom, error) {
	if len(body) < 8 {
		return nil, fmt.Errorf("mp4: data atom body too short (%d bytes)", len(body))
	}
	return &DataAtom{
		TypeCode: binary.BigEndian.Uint32(body[0:4]),
		Locale:   binary.BigEndian.Uint32(body[4:8]),
		Payload:  append([]byte(nil), body[8:]...),
	}, nil
}

// encode returns the data atom body (no header).
func (d *DataAtom) encode() []byte {
	out := make([]byte, 8+len(d.Payload))
	binary.BigEndian.PutUint32(out[0:4], d.TypeCode)
	binary.BigEndian.PutUint32(out[4:8], d.Locale)
	copy(out[8:], d.Payload)
	return out
}

// String returns the payload decoded as UTF-8 when TypeCode == 1,
// otherwise the empty string. Useful for the common text keys.
func (d *DataAtom) String() string {
	if d.TypeCode == DataTypeUTF8 {
		return string(d.Payload)
	}
	return ""
}

// Int reads the payload as a big-endian signed integer of 1, 2, 4
// or 8 bytes (TypeCode 21). Returns an error for other type codes.
func (d *DataAtom) Int() (int64, error) {
	if d.TypeCode != DataTypeBEInt {
		return 0, fmt.Errorf("mp4: data atom is type %d, not int", d.TypeCode)
	}
	switch len(d.Payload) {
	case 0:
		return 0, nil
	case 1:
		return int64(int8(d.Payload[0])), nil
	case 2:
		return int64(int16(binary.BigEndian.Uint16(d.Payload))), nil
	case 4:
		return int64(int32(binary.BigEndian.Uint32(d.Payload))), nil
	case 8:
		return int64(binary.BigEndian.Uint64(d.Payload)), nil
	default:
		return 0, fmt.Errorf("mp4: int payload length %d not in {1,2,4,8}", len(d.Payload))
	}
}

// makeUTF8Data creates a UTF-8 data atom for textual values.
func makeUTF8Data(s string) *DataAtom {
	return &DataAtom{TypeCode: DataTypeUTF8, Payload: []byte(s)}
}

// makeBEIntData creates a big-endian signed-int data atom of the
// shortest size that can hold v.
func makeBEIntData(v int64) *DataAtom {
	switch {
	case v >= -(1<<7) && v < 1<<7:
		return &DataAtom{TypeCode: DataTypeBEInt, Payload: []byte{byte(int8(v))}}
	case v >= -(1<<15) && v < 1<<15:
		var b [2]byte
		binary.BigEndian.PutUint16(b[:], uint16(int16(v)))
		return &DataAtom{TypeCode: DataTypeBEInt, Payload: b[:]}
	case v >= -(1<<31) && v < 1<<31:
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(int32(v)))
		return &DataAtom{TypeCode: DataTypeBEInt, Payload: b[:]}
	default:
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(v))
		return &DataAtom{TypeCode: DataTypeBEInt, Payload: b[:]}
	}
}

// TrackNumber returns (track, total) parsed from a trkn / disk
// payload: 2-byte reserved + 2-byte number + 2-byte total + optional
// 2-byte trailing reserved.
//
// iTunes emits trkn as 8 bytes and disk as 6 bytes (no trailing
// reserved), so anything from 6 bytes upward is accepted here.
func (d *DataAtom) TrackNumber() (track, total uint16, err error) {
	if d.TypeCode != DataTypeBinary {
		return 0, 0, errors.New("mp4: trkn/disk payload is not binary")
	}
	if len(d.Payload) < 6 {
		return 0, 0, fmt.Errorf("mp4: trkn/disk payload %d bytes < 6", len(d.Payload))
	}
	return binary.BigEndian.Uint16(d.Payload[2:4]),
		binary.BigEndian.Uint16(d.Payload[4:6]),
		nil
}

// makeTrackNumberData builds a trkn/disk binary payload.
func makeTrackNumberData(number, total uint16) *DataAtom {
	var b bytes.Buffer
	b.Write(make([]byte, 2))
	binary.Write(&b, binary.BigEndian, number)
	binary.Write(&b, binary.BigEndian, total)
	b.Write(make([]byte, 2))
	return &DataAtom{TypeCode: DataTypeBinary, Payload: b.Bytes()}
}
