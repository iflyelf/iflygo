package agent

import (
	"encoding/binary"
	"math"
	"testing"
)

// decodeADPCM 测试用参考解码器，与 Web 客户端逻辑一致。
func decodeADPCM(data []byte) []int16 {
	var states [2]imaState
	out := make([]int16, 0, len(data)*2)
	for _, b := range data {
		for ch := 0; ch < 2; ch++ {
			nibble := b & 0x0f
			if ch == 1 {
				nibble = b >> 4
			}
			state := &states[ch]
			step := imaStepTable[state.index]
			diff := step >> 3
			if nibble&4 != 0 {
				diff += step
			}
			if nibble&2 != 0 {
				diff += step >> 1
			}
			if nibble&1 != 0 {
				diff += step >> 2
			}
			if nibble&8 != 0 {
				state.predictor -= diff
			} else {
				state.predictor += diff
			}
			if state.predictor > 32767 {
				state.predictor = 32767
			} else if state.predictor < -32768 {
				state.predictor = -32768
			}
			state.index += imaIndexTable[nibble]
			if state.index < 0 {
				state.index = 0
			} else if state.index > 88 {
				state.index = 88
			}
			out = append(out, int16(state.predictor))
		}
	}
	return out
}

func TestADPCMRoundTrip(t *testing.T) {
	const frames = 960
	pcm := make([]byte, frames*4)
	want := make([]int16, 0, frames*2)
	for i := 0; i < frames; i++ {
		left := int16(12000 * math.Sin(2*math.Pi*440*float64(i)/48000))
		right := int16(8000 * math.Sin(2*math.Pi*660*float64(i)/48000))
		binary.LittleEndian.PutUint16(pcm[i*4:], uint16(left))
		binary.LittleEndian.PutUint16(pcm[i*4+2:], uint16(right))
		want = append(want, left, right)
	}
	var encoder adpcmEncoder
	encoded := encoder.encode(pcm)
	if len(encoded) != frames {
		t.Fatalf("encoded length %d want %d", len(encoded), frames)
	}
	got := decodeADPCM(encoded)
	if len(got) != len(want) {
		t.Fatalf("decoded length %d want %d", len(got), len(want))
	}
	var total int64
	var peak int
	for i := range want {
		diff := int(got[i]) - int(want[i])
		if diff < 0 {
			diff = -diff
		}
		if diff > peak {
			peak = diff
		}
		total += int64(diff)
	}
	mean := total / int64(len(want))
	if mean > 600 || peak > 6000 {
		t.Fatalf("adpcm error too high: mean=%d peak=%d", mean, peak)
	}
}

func TestADPCMResetRestoresSilence(t *testing.T) {
	var encoder adpcmEncoder
	silence := make([]byte, audioChunk)
	encoder.encode(silence)
	if got := decodeADPCM(encoder.encode(silence)); len(got) == 0 {
		t.Fatal("no decoded samples")
	}
	encoder.reset()
	if encoder.states[0].predictor != 0 || encoder.states[0].index != 0 {
		t.Fatal("reset did not clear state")
	}
}
