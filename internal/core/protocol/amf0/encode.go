// If you are AI: This file implements AMF0 encoding for RTMP response messages.
// Only encodes types needed for RTMP command responses.

package amf0

import (
	"bytes"
	"encoding/binary"
	"io"
)

// Encode writes an AMF0 value to the writer.
func Encode(w io.Writer, val Value) error {
	switch v := val.(type) {
	case float64:
		return encodeNumber(w, v)
	case bool:
		return encodeBoolean(w, v)
	case string:
		return encodeString(w, v)
	case nil:
		return encodeNull(w)
	case Object:
		return encodeObject(w, v)
	case Array:
		return encodeArray(w, v)
	default:
		// Try number conversion
		if num, ok := val.(float64); ok {
			return encodeNumber(w, num)
		}
		return encodeNull(w)
	}
}

// encodeNumber encodes an AMF0 number.
func encodeNumber(w io.Writer, num float64) error {
	if err := binary.Write(w, binary.BigEndian, byte(TypeNumber)); err != nil {
		return err
	}
	return binary.Write(w, binary.BigEndian, num)
}

// encodeBoolean encodes an AMF0 boolean.
func encodeBoolean(w io.Writer, b bool) error {
	if err := binary.Write(w, binary.BigEndian, byte(TypeBoolean)); err != nil {
		return err
	}
	var val byte
	if b {
		val = 1
	}
	return binary.Write(w, binary.BigEndian, val)
}

// encodeString encodes an AMF0 string.
func encodeString(w io.Writer, s string) error {
	if err := binary.Write(w, binary.BigEndian, byte(TypeString)); err != nil {
		return err
	}
	length := uint16(len(s))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return err
	}
	_, err := w.Write([]byte(s))
	return err
}

// encodeNull encodes an AMF0 null.
func encodeNull(w io.Writer) error {
	return binary.Write(w, binary.BigEndian, byte(TypeNull))
}

// encodeObject encodes an AMF0 object.
func encodeObject(w io.Writer, obj Object) error {
	if err := binary.Write(w, binary.BigEndian, byte(TypeObject)); err != nil {
		return err
	}
	for key, val := range obj {
		keyLen := uint16(len(key))
		if err := binary.Write(w, binary.BigEndian, keyLen); err != nil {
			return err
		}
		if _, err := w.Write([]byte(key)); err != nil {
			return err
		}
		if err := Encode(w, val); err != nil {
			return err
		}
	}
	// Object end marker
	if err := binary.Write(w, binary.BigEndian, uint16(0)); err != nil {
		return err
	}
	return binary.Write(w, binary.BigEndian, byte(TypeObjectEnd))
}

// encodeArray encodes an AMF0 strict array.
func encodeArray(w io.Writer, arr Array) error {
	if err := binary.Write(w, binary.BigEndian, byte(TypeStrictArray)); err != nil {
		return err
	}
	count := uint32(len(arr))
	if err := binary.Write(w, binary.BigEndian, count); err != nil {
		return err
	}
	for _, val := range arr {
		if err := Encode(w, val); err != nil {
			return err
		}
	}
	return nil
}

// EncodeCommand encodes an AMF0 command (strict array) to bytes.
func EncodeCommand(arr Array) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeArray(&buf, arr); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
