// If you are AI: This file implements AMF0 decoding for RTMP command messages.
// Only decodes types needed for RTMP publish commands.

package amf0

import (
	"encoding/binary"
	"errors"
	"io"
)

var (
	ErrUnexpectedType = errors.New("unexpected AMF0 type")
	ErrInvalidData    = errors.New("invalid AMF0 data")
)

// Decode reads and decodes a single AMF0 value from the reader.
// Returns the decoded value and any error.
func Decode(r io.Reader) (Value, error) {
	var typeMarker byte
	if err := binary.Read(r, binary.BigEndian, &typeMarker); err != nil {
		return nil, err
	}

	switch typeMarker {
	case TypeNumber:
		return decodeNumber(r)
	case TypeBoolean:
		return decodeBoolean(r)
	case TypeString:
		return decodeString(r)
	case TypeNull, TypeUndefined:
		return nil, nil
	case TypeObject:
		return decodeObject(r)
	case TypeECMAArray:
		return decodeECMAArray(r)
	default:
		return nil, ErrUnexpectedType
	}
}

// DecodeString reads an AMF0 string value.
func DecodeString(r io.Reader) (string, error) {
	var typeMarker byte
	if err := binary.Read(r, binary.BigEndian, &typeMarker); err != nil {
		return "", err
	}
	if typeMarker != TypeString {
		return "", ErrUnexpectedType
	}
	return decodeString(r)
}

// decodeNumber decodes an AMF0 number (double precision float64).
func decodeNumber(r io.Reader) (float64, error) {
	var num float64
	err := binary.Read(r, binary.BigEndian, &num)
	return num, err
}

// decodeBoolean decodes an AMF0 boolean.
func decodeBoolean(r io.Reader) (bool, error) {
	var b byte
	if err := binary.Read(r, binary.BigEndian, &b); err != nil {
		return false, err
	}
	return b != 0, nil
}

// decodeString decodes an AMF0 string.
func decodeString(r io.Reader) (string, error) {
	var length uint16
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return "", err
	}
	if length == 0 {
		return "", nil
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// decodeObject decodes an AMF0 object.
func decodeObject(r io.Reader) (Object, error) {
	obj := make(Object)
	for {
		var keyLen uint16
		if err := binary.Read(r, binary.BigEndian, &keyLen); err != nil {
			return nil, err
		}
		if keyLen == 0 {
			// Object end marker
			var endMarker byte
			if err := binary.Read(r, binary.BigEndian, &endMarker); err != nil {
				return nil, err
			}
			if endMarker != TypeObjectEnd {
				return nil, ErrInvalidData
			}
			break
		}
		keyBuf := make([]byte, keyLen)
		if _, err := io.ReadFull(r, keyBuf); err != nil {
			return nil, err
		}
		key := string(keyBuf)
		value, err := Decode(r)
		if err != nil {
			return nil, err
		}
		obj[key] = value
	}
	return obj, nil
}

// decodeECMAArray decodes an AMF0 ECMA array.
func decodeECMAArray(r io.Reader) (Object, error) {
	var count uint32
	if err := binary.Read(r, binary.BigEndian, &count); err != nil {
		return nil, err
	}
	// ECMA arrays are decoded as objects
	return decodeObject(r)
}

// DecodeCommand decodes an AMF0 command message (array of values).
func DecodeCommand(r io.Reader) (Array, error) {
	var typeMarker byte
	if err := binary.Read(r, binary.BigEndian, &typeMarker); err != nil {
		return nil, err
	}
	if typeMarker != TypeStrictArray {
		return nil, ErrUnexpectedType
	}
	var count uint32
	if err := binary.Read(r, binary.BigEndian, &count); err != nil {
		return nil, err
	}
	arr := make(Array, count)
	for i := uint32(0); i < count; i++ {
		val, err := Decode(r)
		if err != nil {
			return nil, err
		}
		arr[i] = val
	}
	return arr, nil
}
