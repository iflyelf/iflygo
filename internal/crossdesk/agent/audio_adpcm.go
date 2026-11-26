package agent

import "encoding/binary"

// IMA ADPCM（DVI4）编码：每帧 20ms，每字节携带左右声道各 1 个 4bit 样本。
// 48kHz 双声道约 384 kbps，相比 s16le PCM 降低 4 倍。
const (
	audioFormatPCM        = 1
	audioFormatADPCM      = 2
	audioFormatADPCMReset = 3
)

var imaStepTable = [89]int32{
	7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 19, 21, 23, 25, 28, 31, 34, 37, 41, 45,
	50, 55, 60, 66, 73, 80, 88, 97, 107, 118, 130, 143, 157, 173, 190, 209, 230,
	253, 279, 307, 337, 371, 408, 449, 494, 544, 598, 658, 724, 796, 876, 963,
	1060, 1166, 1282, 1411, 1552, 1707, 1878, 2066, 2272, 2499, 2749, 3024, 3327,
	3660, 4026, 4428, 4871, 5358, 5894, 6484, 7132, 7845, 8630, 9493, 10442,
	11487, 12635, 13899, 15289, 16818, 18500, 20350, 22385, 24623, 27086, 29794,
	32767,
}

var imaIndexTable = [16]int32{-1, -1, -1, -1, 2, 4, 6, 8, -1, -1, -1, -1, 2, 4, 6, 8}

type imaState struct {
	predictor int32
	index     int32
}

type adpcmEncoder struct {
	states [2]imaState
}

func (e *adpcmEncoder) reset() {
	e.states = [2]imaState{}
}

// encode 输入交错 s16le 双声道 PCM，输出每字节一个立体声帧的 ADPCM。
func (e *adpcmEncoder) encode(pcm []byte) []byte {
	frames := len(pcm) / 4
	out := make([]byte, frames)
	for i := 0; i < frames; i++ {
		left := int32(int16(binary.LittleEndian.Uint16(pcm[i*4:])))
		right := int32(int16(binary.LittleEndian.Uint16(pcm[i*4+2:])))
		out[i] = e.sample(0, left) | e.sample(1, right)<<4
	}
	return out
}

func (e *adpcmEncoder) sample(channel int, sample int32) byte {
	state := &e.states[channel]
	step := imaStepTable[state.index]
	diff := sample - state.predictor
	var nibble byte
	if diff < 0 {
		nibble = 8
		diff = -diff
	}
	if diff >= step {
		nibble |= 4
		diff -= step
	}
	if diff >= step>>1 {
		nibble |= 2
		diff -= step >> 1
	}
	if diff >= step>>2 {
		nibble |= 1
	}
	e.update(state, nibble)
	return nibble
}

func (e *adpcmEncoder) update(state *imaState, nibble byte) {
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
}
