package audio

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
)

// pcm is decoded mono audio plus its format metadata.
type pcm struct {
	samples    []float64 // mono, normalised to [-1, 1]
	sampleRate int
	channels   int
	bitDepth   int
}

var errNoPCM = errors.New("no decodable PCM")

// parsePCMInMemory конвертирует уже загруженный raw-байтовый буфер PCM в моно float64.
// Нет IO — только арифметика в памяти. Это горячий путь после одиночного io.ReadFull.
func parsePCMInMemory(raw []byte, format, channels, sampleRate, bits, maxSamples int) (*pcm, error) {
	if channels <= 0 {
		channels = 1
	}
	bps := (bits + 7) / 8
	if bps <= 0 {
		return nil, errNoPCM
	}
	frameBytes := bps * channels
	if frameBytes == 0 {
		return nil, errNoPCM
	}
	totalFrames := len(raw) / frameBytes
	if maxSamples > 0 && totalFrames > maxSamples {
		totalFrames = maxSamples
	}
	if totalFrames == 0 {
		return nil, errNoPCM
	}
	out := &pcm{sampleRate: sampleRate, channels: channels, bitDepth: bits}
	out.samples = make([]float64, totalFrames)
	for i := 0; i < totalFrames; i++ {
		off := i * frameBytes
		var acc float64
		for c := 0; c < channels; c++ {
			acc += sampleToFloat(raw[off+c*bps:off+(c+1)*bps], format, bits)
		}
		out.samples[i] = acc / float64(channels)
	}
	return out, nil
}

// decodeWAV parses a RIFF/WAVE file (PCM and IEEE float) into mono samples.
// It reads at most maxSamples frames to bound memory on huge files.
func decodeWAV(f *os.File, maxSamples int) (*pcm, error) {
	var riff [12]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return nil, err
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return nil, errNoPCM
	}

	var (
		audioFormat   uint16
		channels      uint16
		sampleRate    uint32
		bitsPerSample uint16
		haveFmt       bool
	)
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(f, hdr); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}
		id := string(hdr[0:4])
		size := binary.LittleEndian.Uint32(hdr[4:8])
		switch id {
		case "fmt ":
			buf := make([]byte, size)
			if _, err := io.ReadFull(f, buf); err != nil {
				return nil, err
			}
			audioFormat = binary.LittleEndian.Uint16(buf[0:2])
			channels = binary.LittleEndian.Uint16(buf[2:4])
			sampleRate = binary.LittleEndian.Uint32(buf[4:8])
			bitsPerSample = binary.LittleEndian.Uint16(buf[14:16])
			haveFmt = true
		case "data":
			if !haveFmt {
				return nil, errNoPCM
			}
			// Читаем весь data-chunk одним вызовом — устраняет per-frame syscalls.
			// Ограничиваем по maxSamples, чтобы не читать гигабайты для длинных WAV.
			readBytes := int(size)
			if maxSamples > 0 {
				frameBytes := ((int(bitsPerSample)+7)/8) * int(channels)
				if frameBytes > 0 {
					maxBytes := maxSamples * frameBytes
					if maxBytes < readBytes {
						readBytes = maxBytes
					}
				}
			}
			raw := make([]byte, readBytes)
			n, _ := io.ReadFull(f, raw)
			if n == 0 {
				return nil, errNoPCM
			}
			return parsePCMInMemory(raw[:n], int(audioFormat), int(channels), int(sampleRate), int(bitsPerSample), maxSamples)
		default:
			// skip unknown chunk (account for word alignment padding)
			skip := int64(size)
			if size%2 == 1 {
				skip++
			}
			if _, err := f.Seek(skip, io.SeekCurrent); err != nil {
				return nil, err
			}
		}
	}
	return nil, errNoPCM
}

func readPCMData(r io.Reader, dataSize int, format, channels, sampleRate, bits, maxSamples int) (*pcm, error) {
	if channels <= 0 {
		channels = 1
	}
	bytesPerSample := bits / 8
	if bytesPerSample <= 0 {
		return nil, errNoPCM
	}
	totalFrames := dataSize / (bytesPerSample * channels)
	out := &pcm{sampleRate: sampleRate, channels: channels, bitDepth: bits}

	buf := make([]byte, bytesPerSample*channels)
	count := 0
	for count < totalFrames {
		if _, err := io.ReadFull(r, buf); err != nil {
			break
		}
		var acc float64
		for c := 0; c < channels; c++ {
			acc += sampleToFloat(buf[c*bytesPerSample:(c+1)*bytesPerSample], format, bits)
		}
		out.samples = append(out.samples, acc/float64(channels))
		count++
		if maxSamples > 0 && count >= maxSamples {
			break
		}
	}
	if len(out.samples) == 0 {
		return nil, errNoPCM
	}
	return out, nil
}

func sampleToFloat(b []byte, format, bits int) float64 {
	// format 3 == IEEE float
	if format == 3 {
		switch bits {
		case 32:
			return float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
		case 64:
			return math.Float64frombits(binary.LittleEndian.Uint64(b))
		}
	}
	switch bits {
	case 8:
		return (float64(b[0]) - 128) / 128
	case 16:
		return float64(int16(binary.LittleEndian.Uint16(b))) / 32768
	case 24:
		v := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
		if v&0x800000 != 0 {
			v |= ^0xffffff
		}
		return float64(v) / 8388608
	case 32:
		return float64(int32(binary.LittleEndian.Uint32(b))) / 2147483648
	}
	return 0
}

// decodeAIFF parses an AIFF/AIFF-C file (big-endian PCM) into mono samples.
func decodeAIFF(f *os.File, maxSamples int) (*pcm, error) {
	var form [12]byte
	if _, err := io.ReadFull(f, form[:]); err != nil {
		return nil, err
	}
	if string(form[0:4]) != "FORM" || (string(form[8:12]) != "AIFF" && string(form[8:12]) != "AIFC") {
		return nil, errNoPCM
	}
	var (
		channels   int
		sampleRate int
		bits       int
		haveComm   bool
	)
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(f, hdr); err != nil {
			break
		}
		id := string(hdr[0:4])
		size := int(binary.BigEndian.Uint32(hdr[4:8]))
		switch id {
		case "COMM":
			buf := make([]byte, size)
			if _, err := io.ReadFull(f, buf); err != nil {
				return nil, err
			}
			channels = int(binary.BigEndian.Uint16(buf[0:2]))
			bits = int(binary.BigEndian.Uint16(buf[6:8]))
			sampleRate = int(extFloat80(buf[8:18]))
			haveComm = true
		case "SSND":
			if !haveComm {
				return nil, errNoPCM
			}
			ob := make([]byte, 8) // offset + blockSize поля перед данными
			if _, err := io.ReadFull(f, ob); err != nil {
				return nil, err
			}
			soundSize := size - 8
			// Читаем весь sound-chunk одним вызовом, ограничивая по maxSamples.
			readBytes := soundSize
			if maxSamples > 0 {
				bps := (bits + 7) / 8
				if bps > 0 && channels > 0 {
					maxBytes := maxSamples * bps * channels
					if maxBytes < readBytes {
						readBytes = maxBytes
					}
				}
			}
			raw := make([]byte, readBytes)
			n, _ := io.ReadFull(f, raw)
			if n == 0 {
				return nil, errNoPCM
			}
			return parseAIFFInMemory(raw[:n], channels, sampleRate, bits, maxSamples)
		default:
			skip := int64(size)
			if size%2 == 1 {
				skip++
			}
			if _, err := f.Seek(skip, io.SeekCurrent); err != nil {
				return nil, err
			}
		}
	}
	return nil, errNoPCM
}

// parseAIFFInMemory обрабатывает big-endian AIFF sound data из буфера в памяти.
func parseAIFFInMemory(raw []byte, channels, sampleRate, bits, maxSamples int) (*pcm, error) {
	if channels <= 0 {
		channels = 1
	}
	bps := (bits + 7) / 8
	if bps <= 0 {
		return nil, errNoPCM
	}
	frameBytes := bps * channels
	if frameBytes == 0 {
		return nil, errNoPCM
	}
	totalFrames := len(raw) / frameBytes
	if maxSamples > 0 && totalFrames > maxSamples {
		totalFrames = maxSamples
	}
	if totalFrames == 0 {
		return nil, errNoPCM
	}
	out := &pcm{sampleRate: sampleRate, channels: channels, bitDepth: bits}
	out.samples = make([]float64, totalFrames)
	for i := 0; i < totalFrames; i++ {
		off := i * frameBytes
		var acc float64
		for c := 0; c < channels; c++ {
			acc += aiffSampleToFloat(raw[off+c*bps:off+(c+1)*bps], bits)
		}
		out.samples[i] = acc / float64(channels)
	}
	return out, nil
}

func readAIFFData(r io.Reader, dataSize, channels, sampleRate, bits, maxSamples int) (*pcm, error) {
	if channels <= 0 {
		channels = 1
	}
	bps := bits / 8
	if bps <= 0 {
		return nil, errNoPCM
	}
	frames := dataSize / (bps * channels)
	out := &pcm{sampleRate: sampleRate, channels: channels, bitDepth: bits}
	buf := make([]byte, bps*channels)
	for n := 0; n < frames; n++ {
		if _, err := io.ReadFull(r, buf); err != nil {
			break
		}
		var acc float64
		for c := 0; c < channels; c++ {
			acc += aiffSampleToFloat(buf[c*bps:(c+1)*bps], bits)
		}
		out.samples = append(out.samples, acc/float64(channels))
		if maxSamples > 0 && len(out.samples) >= maxSamples {
			break
		}
	}
	if len(out.samples) == 0 {
		return nil, errNoPCM
	}
	return out, nil
}

func aiffSampleToFloat(b []byte, bits int) float64 {
	switch bits {
	case 8:
		return float64(int8(b[0])) / 128
	case 16:
		return float64(int16(binary.BigEndian.Uint16(b))) / 32768
	case 24:
		v := int32(b[2]) | int32(b[1])<<8 | int32(b[0])<<16
		if v&0x800000 != 0 {
			v |= ^0xffffff
		}
		return float64(v) / 8388608
	case 32:
		return float64(int32(binary.BigEndian.Uint32(b))) / 2147483648
	}
	return 0
}

// extFloat80 decodes an 80-bit IEEE extended float (used for AIFF sample rate).
func extFloat80(b []byte) float64 {
	if len(b) < 10 {
		return 0
	}
	sign := 1.0
	if b[0]&0x80 != 0 {
		sign = -1.0
	}
	exp := (int(b[0]&0x7f) << 8) | int(b[1])
	mant := binary.BigEndian.Uint64(b[2:10])
	if exp == 0 && mant == 0 {
		return 0
	}
	return sign * float64(mant) * math.Pow(2, float64(exp-16383-63))
}
