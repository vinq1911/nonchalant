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

// SkipAny skips over any AMF0 value without decoding it.
// This allows us to skip complex types we don't need to parse.
func SkipAny(r io.Reader) error {
	var typeMarker byte
	if err := binary.Read(r, binary.BigEndian, &typeMarker); err != nil {
		return err
	}

	switch typeMarker {
	case TypeNumber:
		// Skip 8 bytes (double)
		var buf [8]byte
		_, err := io.ReadFull(r, buf[:])
		return err
	case TypeBoolean:
		// Skip 1 byte
		var b byte
		return binary.Read(r, binary.BigEndian, &b)
	case TypeString:
		// Read length, then skip that many bytes
		var length uint16
		if err := binary.Read(r, binary.BigEndian, &length); err != nil {
			return err
		}
		if length > 0 {
			buf := make([]byte, length)
			_, err := io.ReadFull(r, buf)
			return err
		}
		return nil
	case TypeObject:
		// Skip object key-value pairs until object end marker
		for {
			var keyLen uint16
			if err := binary.Read(r, binary.BigEndian, &keyLen); err != nil {
				return err
			}
			if keyLen == 0 {
				// Object end marker
				var endMarker byte
				return binary.Read(r, binary.BigEndian, &endMarker)
			}
			// Skip key
			keyBuf := make([]byte, keyLen)
			if _, err := io.ReadFull(r, keyBuf); err != nil {
				return err
			}
			// Skip value (recursive)
			if err := SkipAny(r); err != nil {
				return err
			}
		}
	case TypeECMAArray:
		// Skip count (4 bytes), then skip as object
		var count uint32
		if err := binary.Read(r, binary.BigEndian, &count); err != nil {
			return err
		}
		// ECMA arrays are structured like objects
		return SkipAny(r) // Will skip as object
	case TypeStrictArray:
		// Read count, then skip each element
		var count uint32
		if err := binary.Read(r, binary.BigEndian, &count); err != nil {
			return err
		}
		for i := uint32(0); i < count; i++ {
			if err := SkipAny(r); err != nil {
				return err
			}
		}
		return nil
	case TypeNull, TypeUndefined:
		// No data to skip
		return nil
	case TypeLongString:
		// Read length (4 bytes), then skip that many bytes
		var length uint32
		if err := binary.Read(r, binary.BigEndian, &length); err != nil {
			return err
		}
		if length > 0 {
			buf := make([]byte, length)
			_, err := io.ReadFull(r, buf)
			return err
		}
		return nil
	case 0x11: // AVMPlus object marker (AMF3 switch)
		// Skip AMF3 data (for now, just skip the marker)
		// NOTE: Full AMF3 support would require decoding, but we can skip it
		return nil
	default:
		return ErrUnexpectedType
	}
}

// DecodeCommand decodes an AMF0 command message.
// RTMP commands are a sequence of AMF0 values.
// Format: command_name (string), transaction_id (number), command_object (object/null), ...args
// We decode the first two values (name, transaction ID) and skip the rest.
func DecodeCommand(r io.Reader) (Array, error) {
	arr := make(Array, 0, 4)

	// Read command name (required)
	cmdName, err := Decode(r)
	if err != nil {
		return nil, err
	}
	arr = append(arr, cmdName)

	// Read transaction ID (required)
	transID, err := Decode(r)
	if err != nil {
		// If we got command name, return what we have
		return arr, nil
	}
	arr = append(arr, transID)

	// Skip remaining arguments (command object, etc.)
	// We don't need to parse them for basic functionality
	for {
		err := SkipAny(r)
		if err != nil {
			if err == io.EOF {
				break
			}
			// If we got command name and transaction ID, that's enough
			// Ignore errors when skipping remaining args
			break
		}
	}

	return arr, nil
}
