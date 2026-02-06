// If you are AI: This file tests AMF0 encoding, especially command encoding.
package amf0

import (
	"testing"
)

// TestEncodeCommand_NoStrictArray verifies that EncodeCommand writes items sequentially
// without wrapping them in a StrictArray (0x0A). RTMP command bodies must start with
// the first item's type marker (e.g., 0x02 for string "_result").
func TestEncodeCommand_NoStrictArray(t *testing.T) {
	// Build a connect _result payload (same as server uses)
	response := Array{
		"_result",
		float64(1), // transaction ID
		Object{
			"fmsVer":       "FMS/3,0,1,123",
			"capabilities": float64(31),
		},
		Object{
			"level":       "status",
			"code":        "NetConnection.Connect.Success",
			"description": "Connection succeeded.",
		},
	}

	body, err := EncodeCommand(response)
	if err != nil {
		t.Fatalf("EncodeCommand failed: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("Encoded body is empty")
	}

	// First byte MUST be 0x02 (TypeString) for "_result", NOT 0x0A (TypeStrictArray)
	firstByte := body[0]
	if firstByte == TypeStrictArray {
		t.Fatalf("Command encoding incorrectly wraps items in StrictArray (0x%02x). First byte should be 0x02 (string), got 0x%02x", TypeStrictArray, firstByte)
	}
	if firstByte != TypeString {
		t.Fatalf("Command encoding first byte should be 0x02 (TypeString), got 0x%02x", firstByte)
	}

	// Verify the string "_result" follows
	if len(body) < 9 {
		t.Fatalf("Encoded body too short: %d bytes", len(body))
	}
	// Skip type marker (1 byte) + length (2 bytes) = 3 bytes, then check string
	expectedResult := "_result"
	if string(body[3:3+len(expectedResult)]) != expectedResult {
		t.Errorf("Expected string '_result' after type marker, got: %q", string(body[3:3+len(expectedResult)]))
	}
}

// TestEncodeCommand_CreateStreamResult verifies createStream _result encoding.
func TestEncodeCommand_CreateStreamResult(t *testing.T) {
	response := Array{
		"_result",
		float64(2), // transaction ID
		nil,
		float64(1), // stream ID
	}

	body, err := EncodeCommand(response)
	if err != nil {
		t.Fatalf("EncodeCommand failed: %v", err)
	}

	// First byte must be string marker (0x02), not StrictArray (0x0A)
	if body[0] == TypeStrictArray {
		t.Fatal("Command encoding incorrectly wraps items in StrictArray")
	}
	if body[0] != TypeString {
		t.Fatalf("First byte should be 0x02 (TypeString), got 0x%02x", body[0])
	}
}
