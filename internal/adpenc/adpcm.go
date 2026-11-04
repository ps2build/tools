package internal

import (
	"bytes"
	"fmt"
	"math"

	"github.com/go-audio/audio"
)

const blockSamples = 28

var predictorCoeff = [5][2]float64{
	{0.0, 0.0},
	{-60.0 / 64.0, 0.0},
	{-115.0 / 64.0, 52.0 / 64.0},
	{-98.0 / 64.0, 55.0 / 64.0},
	{-122.0 / 64.0, 60.0 / 64.0},
}

type Options struct {
	Loop bool
	// Channel selects which channel to extract from a multi-channel buffer.
	// -1 = downmix to mono (average). Ignored for mono input.
	Channel int
	// FixTerminalFlags writes the spec-correct 0x01/0x03 flag on the true
	// last data block instead of replicating the original tool's dead-code
	// path that leaves it at 0x00. Off by default for bit-exact parity
	// with bITmASTER's packer.
	FixTerminalFlags bool
}

// predictState mirrors find_predict's static _s_1/_s_2: history of the
// *clamped input* samples, carried across block boundaries.
type predictState struct{ s1, s2 float64 }

// packState mirrors pack's static s_1/s_2: history of the *quantized
// reconstruction*, carried separately from predictState.
type packState struct{ s1, s2 float64 }

func Encode(buf *audio.IntBuffer, opts Options) ([]byte, error) {
	if buf == nil || buf.Format == nil {
		return nil, fmt.Errorf("vag: nil buffer or format")
	}
	samples, err := extractChannel(buf, opts.Channel)
	if err != nil {
		return nil, err
	}
	return encodeSamples(samples, opts.Loop, opts.FixTerminalFlags), nil
}

func extractChannel(buf *audio.IntBuffer, channel int) ([]int16, error) {
	nc := buf.Format.NumChannels
	if nc <= 1 {
		out := make([]int16, len(buf.Data))
		for i, v := range buf.Data {
			out[i] = clamp16(v)
		}
		return out, nil
	}
	n := len(buf.Data) / nc
	out := make([]int16, n)
	if channel < 0 {
		for i := range n {
			sum := 0
			for _, v := range buf.Data[i*nc : i*nc+nc] {
				sum += v
			}
			out[i] = clamp16(sum / nc)
		}
		return out, nil
	}
	if channel >= nc {
		return nil, fmt.Errorf("vag: channel %d out of range (%d channels)", channel, nc)
	}
	for i := range n {
		out[i] = clamp16(buf.Data[i*nc+channel])
	}
	return out, nil
}

func clamp16(v int) int16 {
	return int16(min(max(v, -32768), 32767))
}

func encodeSamples(samples []int16, loop, fixFlags bool) []byte {
	var ps predictState
	var pk packState
	var out bytes.Buffer

	sampleLen := len(samples)
	totalBlocks := (sampleLen + blockSamples - 1) / blockSamples

	flagLoop := 0
	if loop {
		flagLoop = 1
	}
	var flags byte
	var predictNr, shiftFactor int

	remaining := sampleLen
	for j := range totalBlocks {
		var window [blockSamples]int16
		base := j * blockSamples
		copy(window[:], samples[base:min(base+blockSamples, sampleLen)]) // rest stays zero, matches C's blank-fill

		dSamples, pNr, sf := ps.findPredict(window)
		predictNr, shiftFactor = pNr, sf
		fourBit := pk.pack(dSamples, predictNr, shiftFactor)

		out.WriteByte(byte(predictNr<<4) | byte(shiftFactor))

		isLastBlock := j == totalBlocks-1
		switch {
		case flagLoop == 1:
			out.WriteByte(6) // loop start
			flagLoop = 2
		case fixFlags && isLastBlock:
			if flagLoop == 2 {
				out.WriteByte(3) // loop end, jump to repeat start
			} else {
				out.WriteByte(1) // stream end (penultimate-style marker)
			}
		default:
			out.WriteByte(flags) // always 0x00 in unfixed mode
		}

		for k := 0; k < blockSamples; k += 2 {
			d := byte((fourBit[k+1]>>8)&0xf0) | byte((fourBit[k]>>12)&0xf)
			out.WriteByte(d)
		}

		remaining -= blockSamples
		if remaining < blockSamples {
			if flagLoop == 2 {
				flags = 3
			} else {
				flags = 1
			}
		}
	}

	// dedicated terminator block
	out.WriteByte(byte(predictNr<<4) | byte(shiftFactor))
	out.WriteByte(7)
	out.Write(make([]byte, 14))

	return out.Bytes()
}

func (st *predictState) findPredict(samples [blockSamples]int16) (d [blockSamples]float64, predictNr, shiftFactor int) {
	var buffer [blockSamples][5]float64
	var maxAbs [5]float64
	best := 1e10

	// Clamping is predictor-independent, so do it once instead of inside
	// each of the 5 predictor trials below.
	var clamped [blockSamples]float64
	for j, v := range samples {
		clamped[j] = min(max(float64(v), -30720.0), 30719.0)
	}

	for i, coeff := range predictorCoeff {
		s1, s2 := st.s1, st.s2
		for j, s0 := range clamped {
			ds := s0 + s1*coeff[0] + s2*coeff[1]
			buffer[j][i] = ds
			if a := math.Abs(ds); a > maxAbs[i] {
				maxAbs[i] = a
			}
			s2 = s1
			s1 = s0
		}
		if maxAbs[i] < best {
			best = maxAbs[i]
			predictNr = i
		}
		if best <= 7 {
			predictNr = 0
			break
		}
	}

	// History after this block is just the last two clamped samples,
	// regardless of which predictor was chosen.
	st.s1, st.s2 = clamped[blockSamples-1], clamped[blockSamples-2]

	for j := range blockSamples {
		d[j] = buffer[j][predictNr]
	}

	min2 := int(best)
	shiftMask := 0x4000
	shiftFactor = 0
	for shiftFactor < 12 {
		if shiftMask&(min2+(shiftMask>>3)) != 0 {
			break
		}
		shiftFactor++
		shiftMask >>= 1
	}
	return
}

func (pk *packState) pack(d [blockSamples]float64, predictNr, shiftFactor int) [blockSamples]int16 {
	var out [blockSamples]int16
	s1, s2 := pk.s1, pk.s2
	coeff := predictorCoeff[predictNr]
	scale := float64(int(1) << uint(shiftFactor))

	for i, dv := range d {
		s0 := dv + s1*coeff[0] + s2*coeff[1]
		ds := s0 * scale

		di := (int(ds) + 0x800) & ^0xfff // & 0xfffff000 equivalent, sign-safe
		out[i] = clamp16(di)
		di = int(out[i])

		// Go's >> on signed ints is arithmetic per spec, so no portability
		// dance needed here unlike the original C.
		shifted := di >> uint(shiftFactor)
		s2 = s1
		s1 = float64(shifted) - s0
	}
	pk.s1, pk.s2 = s1, s2
	return out
}
