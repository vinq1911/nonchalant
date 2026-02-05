// If you are AI: This file defines AMF0 type constants and basic types.

package amf0

// AMF0 type markers
const (
	TypeNumber      = 0
	TypeBoolean     = 1
	TypeString      = 2
	TypeObject      = 3
	TypeNull        = 5
	TypeUndefined   = 6
	TypeReference   = 7
	TypeECMAArray   = 8
	TypeObjectEnd   = 9
	TypeStrictArray = 10
	TypeDate        = 11
	TypeLongString  = 12
	TypeXMLDocument = 15
	TypeTypedObject = 16
)

// Value represents a decoded AMF0 value.
type Value interface{}

// Object represents an AMF0 object (key-value pairs).
type Object map[string]Value

// Array represents an AMF0 array.
type Array []Value
