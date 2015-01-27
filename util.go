package influxdb

import (
	"fmt"

	"code.google.com/p/log4go"
)

type TimePrecision int

const (
	MicrosecondPrecision TimePrecision = iota
	MillisecondPrecision
	SecondPrecision
)

func parseTimePrecision(s string) (TimePrecision, error) {
	switch s {
	case "u":
		return MicrosecondPrecision, nil
	case "m":
		log4go.Warn("time_precision=m will be disabled in future release, use time_precision=ms instead")
		fallthrough
	case "ms":
		return MillisecondPrecision, nil
	case "s":
		return SecondPrecision, nil
	case "":
		return MillisecondPrecision, nil
	}

	return 0, fmt.Errorf("Unknown time precision %s", s)
}

func hasDuplicates(ss []string) bool {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		if _, ok := m[s]; ok {
			return true
		}
		m[s] = struct{}{}
	}
	return false
}

func removeField(fields []string, name string) []string {
	index := -1
	for idx, field := range fields {
		if field == name {
			index = idx
			break
		}
	}

	if index == -1 {
		return fields
	}

	return append(fields[:index], fields[index+1:]...)
}

func removeTimestampFieldDefinition(fields []string) []string {
	fields = removeField(fields, "time")
	return removeField(fields, "sequence_number")
}

func mapKeyList(m interface{}) []string {

	switch m.(type) {
	case map[string]string:
		return mapStrStrKeyList(m.(map[string]string))
	case map[string]int:
		return mapStrIntKeyList(m.(map[string]int))
	}
	return nil
}

func mapStrStrKeyList(m map[string]string) []string {
	l := make([]string, 0, len(m))
	for k, _ := range m {
		l = append(l, k)
	}
	return l
}

func mapStrIntKeyList(m map[string]int) []string {
	l := make([]string, 0, len(m))
	for k, _ := range m {
		l = append(l, k)
	}
	return l
}
