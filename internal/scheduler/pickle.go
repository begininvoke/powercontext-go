package scheduler

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
	"unicode/utf8"
)

const (
	maxPickleBytes   = 64 * 1024
	maxPickleOpcodes = 4096
	maxPickleMemo    = 512
	maxPickleDepth   = 64
)

type InvalidJobStateError struct{ Detail string }

func (e *InvalidJobStateError) Error() string { return "invalid APScheduler job state: " + e.Detail }

type pickleTuple []any
type pickleGlobal struct{ module, name string }
type pickleObject struct {
	class pickleGlobal
	state map[string]any
}
type pickleTimedelta struct{ days, seconds, microseconds int64 }
type pickleUTC struct{}

type pickleParser struct {
	data     []byte
	position int
	protocol byte
	opcodes  int
	stack    []any
	marks    []int
	memo     []any
	frameEnd int
}

func decodeJobState(blob []byte, columnID, runtimePath string, nextRunColumn float64) (Job, error) {
	root, err := parsePickle(blob)
	if err != nil {
		return Job{}, err
	}
	state, ok := root.(map[string]any)
	if !ok {
		return Job{}, invalidState("root is not a dictionary")
	}
	expectedKeys := []string{
		"version", "id", "func", "trigger", "executor", "args", "kwargs", "name",
		"misfire_grace_time", "coalesce", "max_instances", "next_run_time",
	}
	if !exactKeys(state, expectedKeys) {
		return Job{}, invalidState("Job state fields differ from v1")
	}
	version, ok := state["version"].(int64)
	if !ok || version != 1 {
		return Job{}, invalidState("unsupported Job state version")
	}
	id, ok := state["id"].(string)
	if !ok || id != columnID {
		return Job{}, invalidState("row ID and Job ID differ")
	}
	kind, ok := kindForID(id)
	if !ok {
		return Job{}, invalidState("unknown stable Job ID")
	}
	_, callable, name, _ := jobIdentity(kind)
	if state["func"] != callable || state["executor"] != "default" || state["name"] != name {
		return Job{}, invalidState("Job callable identity is not allowed")
	}
	args, ok := state["args"].(pickleTuple)
	if !ok || len(args) != 1 {
		return Job{}, invalidState("Job args must contain one runtime path")
	}
	storedPath, ok := args[0].(string)
	if !ok || !validStoredRuntimePath(storedPath, runtimePath) {
		return Job{}, invalidState("Job runtime path differs from the scheduler path")
	}
	kwargs, ok := state["kwargs"].(map[string]any)
	if !ok || len(kwargs) != 0 {
		return Job{}, invalidState("Job kwargs must be empty")
	}
	if state["misfire_grace_time"] != nil || state["coalesce"] != true {
		return Job{}, invalidState("Job retry/coalescing policy differs")
	}
	maxInstances, ok := state["max_instances"].(int64)
	if !ok || maxInstances != 1 {
		return Job{}, invalidState("Job max_instances must be one")
	}
	trigger, ok := state["trigger"].(*pickleObject)
	if !ok || trigger.class != (pickleGlobal{"apscheduler.triggers.interval", "IntervalTrigger"}) {
		return Job{}, invalidState("Job trigger is not IntervalTrigger")
	}
	start, interval, err := decodeIntervalTrigger(trigger)
	if err != nil {
		return Job{}, err
	}
	next, ok := state["next_run_time"].(time.Time)
	if !ok {
		return Job{}, invalidState("next_run_time is not an aware UTC datetime")
	}
	job, err := NewJob(kind, runtimePath, interval, start, next)
	if err != nil {
		return Job{}, invalidState(err.Error())
	}
	if !sameTimestamp(nextRunColumn, next) {
		return Job{}, invalidState("next_run_time column and Job state differ")
	}
	return job, nil
}

func decodeIntervalTrigger(object *pickleObject) (time.Time, time.Duration, error) {
	state := object.state
	if !exactKeys(state, []string{"version", "timezone", "start_date", "end_date", "interval", "jitter"}) {
		return time.Time{}, 0, invalidState("IntervalTrigger state fields differ from v2")
	}
	version, ok := state["version"].(int64)
	if !ok || version != 2 {
		return time.Time{}, 0, invalidState("unsupported IntervalTrigger state version")
	}
	if _, ok := state["timezone"].(pickleUTC); !ok {
		return time.Time{}, 0, invalidState("IntervalTrigger timezone is not UTC")
	}
	start, ok := state["start_date"].(time.Time)
	if !ok || start.Location() != time.UTC {
		return time.Time{}, 0, invalidState("IntervalTrigger start_date is not UTC")
	}
	if state["end_date"] != nil || state["jitter"] != nil {
		return time.Time{}, 0, invalidState("IntervalTrigger end_date and jitter must be null")
	}
	interval, ok := state["interval"].(pickleTimedelta)
	if !ok {
		return time.Time{}, 0, invalidState("IntervalTrigger interval is not timedelta")
	}
	duration, err := interval.duration()
	if err != nil || duration <= 0 {
		return time.Time{}, 0, invalidState("IntervalTrigger interval must be positive")
	}
	return start, duration, nil
}

func parsePickle(blob []byte) (any, error) {
	if len(blob) == 0 || len(blob) > maxPickleBytes {
		return nil, invalidState("Pickle exceeds the supported size")
	}
	p := pickleParser{data: blob, frameEnd: -1}
	for {
		if p.opcodes >= maxPickleOpcodes {
			return nil, invalidState("Pickle opcode limit exceeded")
		}
		if p.frameEnd >= 0 {
			if p.position > p.frameEnd {
				return nil, invalidState("Pickle frame boundary was exceeded")
			}
			if p.position == p.frameEnd {
				p.frameEnd = -1
			}
		}
		opcode, err := p.byte()
		if err != nil {
			return nil, err
		}
		p.opcodes++
		switch opcode {
		case 0x80: // PROTO
			protocol, err := p.byte()
			if err != nil {
				return nil, err
			}
			if p.opcodes != 1 || (protocol != 4 && protocol != 5) {
				return nil, invalidState("only Pickle protocol 4/5 is allowed")
			}
			p.protocol = protocol
		case 0x95: // FRAME
			if p.protocol < 4 || p.frameEnd >= 0 {
				return nil, invalidState("invalid Pickle frame")
			}
			length, err := p.uint64()
			if err != nil {
				return nil, err
			}
			if length > uint64(len(p.data)-p.position) {
				return nil, invalidState("truncated Pickle frame")
			}
			p.frameEnd = p.position + int(length)
		case '}':
			p.push(map[string]any{})
		case ')':
			p.push(pickleTuple{})
		case '(':
			if len(p.marks) >= maxPickleDepth {
				return nil, invalidState("Pickle nesting limit exceeded")
			}
			p.marks = append(p.marks, len(p.stack))
		case 'N':
			p.push(nil)
		case 0x88:
			p.push(true)
		case 0x89:
			p.push(false)
		case 'K':
			value, err := p.byte()
			if err != nil {
				return nil, err
			}
			p.push(int64(value))
		case 'M':
			value, err := p.uint16()
			if err != nil {
				return nil, err
			}
			p.push(int64(value))
		case 'J':
			value, err := p.uint32()
			if err != nil {
				return nil, err
			}
			p.push(int64(int32(value)))
		case 0x8a: // LONG1
			length, err := p.byte()
			if err != nil {
				return nil, err
			}
			value, err := p.long(int(length))
			if err != nil {
				return nil, err
			}
			p.push(value)
		case 0x8b: // LONG4
			length, err := p.uint32()
			if err != nil {
				return nil, err
			}
			if length > 8 {
				return nil, invalidState("Pickle integer exceeds int64")
			}
			value, err := p.long(int(length))
			if err != nil {
				return nil, err
			}
			p.push(value)
		case 0x8c: // SHORT_BINUNICODE
			length, err := p.byte()
			if err != nil {
				return nil, err
			}
			value, err := p.string(int(length))
			if err != nil {
				return nil, err
			}
			p.push(value)
		case 'X':
			length, err := p.uint32()
			if err != nil {
				return nil, err
			}
			value, err := p.stringLength(uint64(length))
			if err != nil {
				return nil, err
			}
			p.push(value)
		case 0x8d: // BINUNICODE8
			length, err := p.uint64()
			if err != nil {
				return nil, err
			}
			value, err := p.stringLength(length)
			if err != nil {
				return nil, err
			}
			p.push(value)
		case 'C':
			length, err := p.byte()
			if err != nil {
				return nil, err
			}
			value, err := p.bytes(int(length))
			if err != nil {
				return nil, err
			}
			p.push(value)
		case 'B':
			length, err := p.uint32()
			if err != nil {
				return nil, err
			}
			value, err := p.bytesLength(uint64(length))
			if err != nil {
				return nil, err
			}
			p.push(value)
		case 0x8e: // BINBYTES8
			length, err := p.uint64()
			if err != nil {
				return nil, err
			}
			value, err := p.bytesLength(length)
			if err != nil {
				return nil, err
			}
			p.push(value)
		case 0x93: // STACK_GLOBAL
			name, module, err := p.popStringPair()
			if err != nil {
				return nil, err
			}
			global := pickleGlobal{module: module, name: name}
			if !allowedGlobal(global) {
				return nil, invalidState("Pickle global target is not allowed")
			}
			p.push(global)
		case 0x81: // NEWOBJ
			args, class, err := p.popTupleGlobal()
			if err != nil {
				return nil, err
			}
			if class != (pickleGlobal{"apscheduler.triggers.interval", "IntervalTrigger"}) || len(args) != 0 {
				return nil, invalidState("Pickle NEWOBJ target is not allowed")
			}
			p.push(&pickleObject{class: class})
		case 'R': // REDUCE -- interpreted, never invoked
			args, callable, err := p.popTupleGlobal()
			if err != nil {
				return nil, err
			}
			value, err := safeReduce(callable, args)
			if err != nil {
				return nil, err
			}
			p.push(value)
		case 'b':
			state, err := p.pop()
			if err != nil {
				return nil, err
			}
			if len(p.stack) == 0 {
				return nil, invalidState("BUILD has no instance")
			}
			object, ok := p.stack[len(p.stack)-1].(*pickleObject)
			fields, fieldsOK := state.(map[string]any)
			if !ok || !fieldsOK || object.class != (pickleGlobal{"apscheduler.triggers.interval", "IntervalTrigger"}) {
				return nil, invalidState("Pickle BUILD target is not allowed")
			}
			object.state = fields
		case 0x85:
			if err := p.fixedTuple(1); err != nil {
				return nil, err
			}
		case 0x86:
			if err := p.fixedTuple(2); err != nil {
				return nil, err
			}
		case 0x87:
			if err := p.fixedTuple(3); err != nil {
				return nil, err
			}
		case 't':
			if err := p.markTuple(); err != nil {
				return nil, err
			}
		case 'u':
			if err := p.setItems(); err != nil {
				return nil, err
			}
		case 's':
			if err := p.setItem(); err != nil {
				return nil, err
			}
		case 0x94: // MEMOIZE
			if err := p.memoize(len(p.memo)); err != nil {
				return nil, err
			}
		case 'q':
			index, err := p.byte()
			if err != nil {
				return nil, err
			}
			if err := p.memoize(int(index)); err != nil {
				return nil, err
			}
		case 'r':
			index, err := p.uint32()
			if err != nil {
				return nil, err
			}
			if err := p.memoize(int(index)); err != nil {
				return nil, err
			}
		case 'h':
			index, err := p.byte()
			if err != nil {
				return nil, err
			}
			if err := p.getMemo(int(index)); err != nil {
				return nil, err
			}
		case 'j':
			index, err := p.uint32()
			if err != nil {
				return nil, err
			}
			if err := p.getMemo(int(index)); err != nil {
				return nil, err
			}
		case '.':
			if p.protocol == 0 || len(p.stack) != 1 || len(p.marks) != 0 || p.position != len(p.data) ||
				(p.frameEnd >= 0 && p.position != p.frameEnd) {
				return nil, invalidState("Pickle did not terminate as one framed value")
			}
			return p.stack[0], nil
		default:
			return nil, invalidState(fmt.Sprintf("Pickle opcode 0x%02x is not allowed", opcode))
		}
		if len(p.stack) > maxPickleOpcodes {
			return nil, invalidState("Pickle stack limit exceeded")
		}
	}
}

func safeReduce(callable pickleGlobal, args pickleTuple) (any, error) {
	switch callable {
	case pickleGlobal{"datetime", "timedelta"}:
		if len(args) != 3 {
			return nil, invalidState("timedelta arguments differ")
		}
		days, dOK := args[0].(int64)
		seconds, sOK := args[1].(int64)
		micros, mOK := args[2].(int64)
		if !dOK || !sOK || !mOK || seconds < 0 || seconds >= 86_400 || micros < 0 || micros >= 1_000_000 {
			return nil, invalidState("timedelta arguments are invalid")
		}
		return pickleTimedelta{days: days, seconds: seconds, microseconds: micros}, nil
	case pickleGlobal{"datetime", "timezone"}:
		if len(args) != 1 {
			return nil, invalidState("timezone arguments differ")
		}
		delta, ok := args[0].(pickleTimedelta)
		if !ok || delta != (pickleTimedelta{}) {
			return nil, invalidState("only UTC timezone is allowed")
		}
		return pickleUTC{}, nil
	case pickleGlobal{"datetime", "datetime"}:
		if len(args) != 2 {
			return nil, invalidState("datetime arguments differ")
		}
		packed, bytesOK := args[0].([]byte)
		_, utcOK := args[1].(pickleUTC)
		if !bytesOK || !utcOK {
			return nil, invalidState("datetime must be UTC")
		}
		return decodePythonDatetime(packed)
	default:
		return nil, invalidState("Pickle REDUCE target is not allowed")
	}
}

func decodePythonDatetime(value []byte) (time.Time, error) {
	if len(value) != 10 || value[2]&0x80 != 0 {
		return time.Time{}, invalidState("datetime binary state is invalid")
	}
	year := int(binary.BigEndian.Uint16(value[:2]))
	month, day := time.Month(value[2]), int(value[3])
	hour, minute, second := int(value[4]), int(value[5]), int(value[6])
	microsecond := int(value[7])<<16 | int(value[8])<<8 | int(value[9])
	result := time.Date(year, month, day, hour, minute, second, microsecond*1_000, time.UTC)
	if result.Year() != year || result.Month() != month || result.Day() != day || result.Hour() != hour ||
		result.Minute() != minute || result.Second() != second || microsecond >= 1_000_000 {
		return time.Time{}, invalidState("datetime binary fields are invalid")
	}
	return result, nil
}

func encodeJobState(job Job) ([]byte, error) {
	validated, err := NewJob(job.kind, job.runtimePath, job.interval, job.start, job.next)
	if err != nil {
		return nil, err
	}
	id, callable, name, _ := jobIdentity(validated.kind)
	w := pickleWriter{data: []byte{0x80, 5, '}', '('}}
	w.string("version")
	w.integer(1)
	w.string("id")
	w.string(id)
	w.string("func")
	w.string(callable)
	w.string("trigger")
	w.intervalTrigger(validated)
	w.string("executor")
	w.string("default")
	w.string("args")
	w.string(validated.runtimePath)
	w.opcode(0x85)
	w.string("kwargs")
	w.opcode('}')
	w.string("name")
	w.string(name)
	w.string("misfire_grace_time")
	w.opcode('N')
	w.string("coalesce")
	w.opcode(0x88)
	w.string("max_instances")
	w.integer(1)
	w.string("next_run_time")
	w.datetime(validated.next)
	w.opcode('u')
	w.opcode('.')
	if len(w.data) > maxPickleBytes {
		return nil, invalidState("encoded Pickle exceeds size limit")
	}
	return w.data, nil
}

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
func (w *pickleWriter) global(module, name string) { w.string(module); w.string(name); w.opcode(0x93) }
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
	packed := []byte{byte(value.Year() >> 8), byte(value.Year()), byte(value.Month()), byte(value.Day()), byte(value.Hour()), byte(value.Minute()), byte(value.Second()), byte(value.Nanosecond() / 1_000 >> 16), byte(value.Nanosecond() / 1_000 >> 8), byte(value.Nanosecond() / 1_000)}
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

func (p *pickleParser) byte() (byte, error) {
	if p.position >= len(p.data) {
		return 0, invalidState("truncated Pickle")
	}
	value := p.data[p.position]
	p.position++
	return value, nil
}
func (p *pickleParser) uint16() (uint16, error) {
	value, err := p.bytes(2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(value), nil
}
func (p *pickleParser) uint32() (uint32, error) {
	value, err := p.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(value), nil
}
func (p *pickleParser) uint64() (uint64, error) {
	value, err := p.bytes(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(value), nil
}
func (p *pickleParser) bytes(length int) ([]byte, error) { return p.bytesLength(uint64(length)) }
func (p *pickleParser) bytesLength(length uint64) ([]byte, error) {
	if length > uint64(len(p.data)-p.position) || length > maxPickleBytes {
		return nil, invalidState("truncated or oversized Pickle value")
	}
	result := append([]byte(nil), p.data[p.position:p.position+int(length)]...)
	p.position += int(length)
	return result, nil
}
func (p *pickleParser) string(length int) (string, error) { return p.stringLength(uint64(length)) }
func (p *pickleParser) stringLength(length uint64) (string, error) {
	value, err := p.bytesLength(length)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(value) {
		return "", invalidState("Pickle Unicode value is invalid UTF-8")
	}
	return string(value), nil
}
func (p *pickleParser) long(length int) (int64, error) {
	if length < 1 || length > 8 {
		return 0, invalidState("Pickle integer exceeds int64")
	}
	value, err := p.bytes(length)
	if err != nil {
		return 0, err
	}
	var buffer [8]byte
	fill := byte(0)
	if value[length-1]&0x80 != 0 {
		fill = 0xff
	}
	for i := range buffer {
		buffer[i] = fill
	}
	copy(buffer[:], value)
	return int64(binary.LittleEndian.Uint64(buffer[:])), nil
}
func (p *pickleParser) push(value any) { p.stack = append(p.stack, value) }
func (p *pickleParser) pop() (any, error) {
	if len(p.stack) == 0 {
		return nil, invalidState("Pickle stack underflow")
	}
	index := len(p.stack) - 1
	value := p.stack[index]
	p.stack = p.stack[:index]
	return value, nil
}
func (p *pickleParser) popStringPair() (name, module string, err error) {
	nameValue, err := p.pop()
	if err != nil {
		return "", "", err
	}
	moduleValue, err := p.pop()
	if err != nil {
		return "", "", err
	}
	name, nameOK := nameValue.(string)
	module, moduleOK := moduleValue.(string)
	if !nameOK || !moduleOK {
		return "", "", invalidState("STACK_GLOBAL requires Unicode names")
	}
	return name, module, nil
}
func (p *pickleParser) popTupleGlobal() (pickleTuple, pickleGlobal, error) {
	argsValue, err := p.pop()
	if err != nil {
		return nil, pickleGlobal{}, err
	}
	callableValue, err := p.pop()
	if err != nil {
		return nil, pickleGlobal{}, err
	}
	args, argsOK := argsValue.(pickleTuple)
	callable, callableOK := callableValue.(pickleGlobal)
	if !argsOK || !callableOK {
		return nil, pickleGlobal{}, invalidState("Pickle constructor operands differ")
	}
	return args, callable, nil
}
func (p *pickleParser) fixedTuple(size int) error {
	if len(p.stack) < size {
		return invalidState("Pickle tuple stack underflow")
	}
	start := len(p.stack) - size
	value := append(pickleTuple(nil), p.stack[start:]...)
	p.stack = p.stack[:start]
	p.push(value)
	return nil
}
func (p *pickleParser) markTuple() error {
	if len(p.marks) == 0 {
		return invalidState("Pickle tuple has no mark")
	}
	mark := p.marks[len(p.marks)-1]
	p.marks = p.marks[:len(p.marks)-1]
	value := append(pickleTuple(nil), p.stack[mark:]...)
	p.stack = p.stack[:mark]
	p.push(value)
	return nil
}
func (p *pickleParser) setItems() error {
	if len(p.marks) == 0 {
		return invalidState("Pickle SETITEMS has no mark")
	}
	mark := p.marks[len(p.marks)-1]
	p.marks = p.marks[:len(p.marks)-1]
	if mark < 1 || (len(p.stack)-mark)%2 != 0 {
		return invalidState("Pickle SETITEMS operands differ")
	}
	target, ok := p.stack[mark-1].(map[string]any)
	if !ok {
		return invalidState("Pickle SETITEMS target is not a dictionary")
	}
	for index := mark; index < len(p.stack); index += 2 {
		key, ok := p.stack[index].(string)
		if !ok {
			return invalidState("Pickle dictionary key is not Unicode")
		}
		if _, exists := target[key]; exists {
			return invalidState("Pickle dictionary contains duplicate keys")
		}
		target[key] = p.stack[index+1]
	}
	p.stack = p.stack[:mark]
	return nil
}
func (p *pickleParser) setItem() error {
	if len(p.stack) < 3 {
		return invalidState("Pickle SETITEM stack underflow")
	}
	value, _ := p.pop()
	keyValue, _ := p.pop()
	target, ok := p.stack[len(p.stack)-1].(map[string]any)
	key, keyOK := keyValue.(string)
	if !ok || !keyOK {
		return invalidState("Pickle SETITEM operands differ")
	}
	if _, exists := target[key]; exists {
		return invalidState("Pickle dictionary contains duplicate keys")
	}
	target[key] = value
	return nil
}
func (p *pickleParser) memoize(index int) error {
	if len(p.stack) == 0 || index < 0 || index >= maxPickleMemo {
		return invalidState("Pickle memo limit or stack violation")
	}
	if index > len(p.memo) {
		return invalidState("Pickle memo index is sparse")
	}
	if index == len(p.memo) {
		p.memo = append(p.memo, p.stack[len(p.stack)-1])
		return nil
	}
	p.memo[index] = p.stack[len(p.stack)-1]
	return nil
}
func (p *pickleParser) getMemo(index int) error {
	if index < 0 || index >= len(p.memo) {
		return invalidState("Pickle memo reference is invalid")
	}
	p.push(p.memo[index])
	return nil
}

func (d pickleTimedelta) duration() (time.Duration, error) {
	if d.days < 0 || d.seconds < 0 || d.seconds >= 86_400 || d.microseconds < 0 || d.microseconds >= 1_000_000 {
		return 0, errors.New("invalid timedelta")
	}
	if d.days > math.MaxInt64/(86_400*1_000_000) {
		return 0, errors.New("timedelta overflow")
	}
	micros := d.days*86_400*1_000_000 + d.seconds*1_000_000 + d.microseconds
	if micros > math.MaxInt64/int64(time.Microsecond) {
		return 0, errors.New("timedelta overflow")
	}
	return time.Duration(micros) * time.Microsecond, nil
}
func allowedGlobal(value pickleGlobal) bool {
	return value == (pickleGlobal{"apscheduler.triggers.interval", "IntervalTrigger"}) || value == (pickleGlobal{"datetime", "timezone"}) || value == (pickleGlobal{"datetime", "timedelta"}) || value == (pickleGlobal{"datetime", "datetime"})
}
func exactKeys(value map[string]any, keys []string) bool {
	if len(value) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := value[key]; !ok {
			return false
		}
	}
	return true
}
func invalidState(detail string) error { return &InvalidJobStateError{Detail: detail} }
