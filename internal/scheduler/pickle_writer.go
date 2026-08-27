// Copyright (c) 2026 OceanBase.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scheduler

import (
	"encoding/binary"
	"math"
	"time"
)

// pickleWriter emits the deliberately tiny protocol-5 subset accepted by the
// matching parser. It is not a general-purpose Pickle encoder.
type pickleWriter struct{ data []byte }

func (w *pickleWriter) opcode(value byte) { w.data = append(w.data, value) }

func (w *pickleWriter) string(value string) {
	bytes := []byte(value)
	if len(bytes) <= math.MaxUint8 {
		w.data = append(w.data, 0x8c, byte(len(bytes)))
		w.data = append(w.data, bytes...)
		return
	}
	w.data = append(w.data, 'X')
	w.uint32(uint32(len(bytes)))
	w.data = append(w.data, bytes...)
}

func (w *pickleWriter) bytes(value []byte) {
	if len(value) <= math.MaxUint8 {
		w.data = append(w.data, 'C', byte(len(value)))
		w.data = append(w.data, value...)
		return
	}
	w.data = append(w.data, 'B')
	w.uint32(uint32(len(value)))
	w.data = append(w.data, value...)
}

func (w *pickleWriter) integer(value int64) {
	if value >= 0 && value <= math.MaxUint8 {
		w.data = append(w.data, 'K', byte(value))
		return
	}
	if value >= 0 && value <= math.MaxUint16 {
		w.data = append(w.data, 'M')
		w.uint16(uint16(value))
		return
	}
	if value >= math.MinInt32 && value <= math.MaxInt32 {
		w.data = append(w.data, 'J')
		w.uint32(uint32(int32(value)))
		return
	}
	var buffer [8]byte
	binary.LittleEndian.PutUint64(buffer[:], uint64(value))
	length := 8
	for length > 1 {
		last, previous := buffer[length-1], buffer[length-2]
		if (last == 0 && previous&0x80 == 0) || (last == 0xff && previous&0x80 != 0) {
			length--
		} else {
			break
		}
	}
	w.data = append(w.data, 0x8a, byte(length))
	w.data = append(w.data, buffer[:length]...)
}

func (w *pickleWriter) global(module, name string) {
	w.string(module)
	w.string(name)
	w.opcode(0x93)
}

func (w *pickleWriter) timedelta(duration time.Duration) {
	micros := duration.Microseconds()
	days := micros / (86_400 * 1_000_000)
	micros -= days * 86_400 * 1_000_000
	seconds := micros / 1_000_000
	micros -= seconds * 1_000_000
	w.global("datetime", "timedelta")
	w.integer(days)
	w.integer(seconds)
	w.integer(micros)
	w.opcode(0x87)
	w.opcode('R')
}

func (w *pickleWriter) utc() {
	w.global("datetime", "timezone")
	w.timedelta(0)
	w.opcode(0x85)
	w.opcode('R')
}

func (w *pickleWriter) datetime(value time.Time) {
	value = value.UTC()
	microsecond := value.Nanosecond() / 1_000
	packed := []byte{
		byte(value.Year() >> 8), byte(value.Year()), byte(value.Month()), byte(value.Day()),
		byte(value.Hour()), byte(value.Minute()), byte(value.Second()),
		byte(microsecond >> 16), byte(microsecond >> 8), byte(microsecond),
	}
	w.global("datetime", "datetime")
	w.bytes(packed)
	w.utc()
	w.opcode(0x86)
	w.opcode('R')
}

func (w *pickleWriter) intervalTrigger(job Job) {
	w.global("apscheduler.triggers.interval", "IntervalTrigger")
	w.opcode(')')
	w.opcode(0x81)
	w.opcode('}')
	w.opcode('(')
	w.string("version")
	w.integer(2)
	w.string("timezone")
	w.utc()
	w.string("start_date")
	w.datetime(job.start)
	w.string("end_date")
	w.opcode('N')
	w.string("interval")
	w.timedelta(job.interval)
	w.string("jitter")
	w.opcode('N')
	w.opcode('u')
	w.opcode('b')
}

func (w *pickleWriter) uint16(value uint16) {
	var buffer [2]byte
	binary.LittleEndian.PutUint16(buffer[:], value)
	w.data = append(w.data, buffer[:]...)
}

func (w *pickleWriter) uint32(value uint32) {
	var buffer [4]byte
	binary.LittleEndian.PutUint32(buffer[:], value)
	w.data = append(w.data, buffer[:]...)
}
