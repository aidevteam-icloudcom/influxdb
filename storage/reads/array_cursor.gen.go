// Generated by tmpl
// https://github.com/benbjohnson/tmpl
//
// DO NOT EDIT!
// Source: array_cursor.gen.go.tmpl

package reads

import (
	"errors"
	"fmt"
	"math"

	"github.com/influxdata/flux/interval"
	"github.com/influxdata/flux/values"
	"github.com/influxdata/influxdb/tsdb/cursors"
)

const (
	// MaxPointsPerBlock is the maximum number of points in an encoded
	// block in a TSM file. It should match the value in the tsm1
	// package, but we don't want to import it.
	MaxPointsPerBlock = 1000
)

func newLimitArrayCursor(cur cursors.Cursor) cursors.Cursor {
	switch cur := cur.(type) {

	case cursors.FloatArrayCursor:
		return newFloatLimitArrayCursor(cur)

	case cursors.IntegerArrayCursor:
		return newIntegerLimitArrayCursor(cur)

	case cursors.UnsignedArrayCursor:
		return newUnsignedLimitArrayCursor(cur)

	case cursors.StringArrayCursor:
		return newStringLimitArrayCursor(cur)

	case cursors.BooleanArrayCursor:
		return newBooleanLimitArrayCursor(cur)

	default:
		panic(fmt.Sprintf("unreachable: %T", cur))
	}
}

// ********************
// Float Array Cursor

type floatArrayFilterCursor struct {
	cursors.FloatArrayCursor
	cond expression
	m    *singleValue
	res  *cursors.FloatArray
	tmp  *cursors.FloatArray
}

func newFloatFilterArrayCursor(cond expression) *floatArrayFilterCursor {
	return &floatArrayFilterCursor{
		cond: cond,
		m:    &singleValue{},
		res:  cursors.NewFloatArrayLen(MaxPointsPerBlock),
		tmp:  &cursors.FloatArray{},
	}
}

func (c *floatArrayFilterCursor) reset(cur cursors.FloatArrayCursor) {
	c.FloatArrayCursor = cur
	c.tmp.Timestamps, c.tmp.Values = nil, nil
}

func (c *floatArrayFilterCursor) Stats() cursors.CursorStats { return c.FloatArrayCursor.Stats() }

func (c *floatArrayFilterCursor) Next() *cursors.FloatArray {
	pos := 0
	c.res.Timestamps = c.res.Timestamps[:cap(c.res.Timestamps)]
	c.res.Values = c.res.Values[:cap(c.res.Values)]

	var a *cursors.FloatArray

	if c.tmp.Len() > 0 {
		a = c.tmp
	} else {
		a = c.FloatArrayCursor.Next()
	}

LOOP:
	for len(a.Timestamps) > 0 {
		for i, v := range a.Values {
			c.m.v = v
			if c.cond.EvalBool(c.m) {
				c.res.Timestamps[pos] = a.Timestamps[i]
				c.res.Values[pos] = v
				pos++
				if pos >= MaxPointsPerBlock {
					c.tmp.Timestamps = a.Timestamps[i+1:]
					c.tmp.Values = a.Values[i+1:]
					break LOOP
				}
			}
		}

		// Clear buffered timestamps & values if we make it through a cursor.
		// The break above will skip this if a cursor is partially read.
		c.tmp.Timestamps = nil
		c.tmp.Values = nil

		a = c.FloatArrayCursor.Next()
	}

	c.res.Timestamps = c.res.Timestamps[:pos]
	c.res.Values = c.res.Values[:pos]

	return c.res
}

type floatMultiShardArrayCursor struct {
	cursors.FloatArrayCursor
	cursorContext
	filter *floatArrayFilterCursor
}

func (c *floatMultiShardArrayCursor) reset(cur cursors.FloatArrayCursor, itrs cursors.CursorIterators, cond expression) {
	if cond != nil {
		if c.filter == nil {
			c.filter = newFloatFilterArrayCursor(cond)
		}
		c.filter.reset(cur)
		cur = c.filter
	}

	c.FloatArrayCursor = cur
	c.itrs = itrs
	c.err = nil
}

func (c *floatMultiShardArrayCursor) Err() error { return c.err }

func (c *floatMultiShardArrayCursor) Stats() cursors.CursorStats {
	return c.FloatArrayCursor.Stats()
}

func (c *floatMultiShardArrayCursor) Next() *cursors.FloatArray {
	for {
		a := c.FloatArrayCursor.Next()
		if a.Len() == 0 {
			if c.nextArrayCursor() {
				continue
			}
		}
		return a
	}
}

func (c *floatMultiShardArrayCursor) nextArrayCursor() bool {
	if len(c.itrs) == 0 {
		return false
	}

	c.FloatArrayCursor.Close()

	var itr cursors.CursorIterator
	var cur cursors.Cursor
	for cur == nil && len(c.itrs) > 0 {
		itr, c.itrs = c.itrs[0], c.itrs[1:]
		cur, _ = itr.Next(c.ctx, c.req)
	}

	var ok bool
	if cur != nil {
		var next cursors.FloatArrayCursor
		next, ok = cur.(cursors.FloatArrayCursor)
		if !ok {
			cur.Close()
			next = FloatEmptyArrayCursor
			c.err = errors.New("expected float cursor")
		} else {
			if c.filter != nil {
				c.filter.reset(next)
				next = c.filter
			}
		}
		c.FloatArrayCursor = next
	} else {
		c.FloatArrayCursor = FloatEmptyArrayCursor
	}

	return ok
}

type floatLimitArrayCursor struct {
	cursors.FloatArrayCursor
	res  *cursors.FloatArray
	done bool
}

func newFloatLimitArrayCursor(cur cursors.FloatArrayCursor) *floatLimitArrayCursor {
	return &floatLimitArrayCursor{
		FloatArrayCursor: cur,
		res:              cursors.NewFloatArrayLen(1),
	}
}

func (c *floatLimitArrayCursor) Stats() cursors.CursorStats { return c.FloatArrayCursor.Stats() }

func (c *floatLimitArrayCursor) Next() *cursors.FloatArray {
	if c.done {
		return &cursors.FloatArray{}
	}
	a := c.FloatArrayCursor.Next()
	if len(a.Timestamps) == 0 {
		return a
	}
	c.done = true
	c.res.Timestamps[0] = a.Timestamps[0]
	c.res.Values[0] = a.Values[0]
	return c.res
}

type floatEmptyArrayCursor struct {
	res cursors.FloatArray
}

var FloatEmptyArrayCursor cursors.FloatArrayCursor = &floatEmptyArrayCursor{}

func (c *floatEmptyArrayCursor) Err() error                 { return nil }
func (c *floatEmptyArrayCursor) Close()                     {}
func (c *floatEmptyArrayCursor) Stats() cursors.CursorStats { return cursors.CursorStats{} }
func (c *floatEmptyArrayCursor) Next() *cursors.FloatArray  { return &c.res }

// ********************
// Integer Array Cursor

type integerArrayFilterCursor struct {
	cursors.IntegerArrayCursor
	cond expression
	m    *singleValue
	res  *cursors.IntegerArray
	tmp  *cursors.IntegerArray
}

func newIntegerFilterArrayCursor(cond expression) *integerArrayFilterCursor {
	return &integerArrayFilterCursor{
		cond: cond,
		m:    &singleValue{},
		res:  cursors.NewIntegerArrayLen(MaxPointsPerBlock),
		tmp:  &cursors.IntegerArray{},
	}
}

func (c *integerArrayFilterCursor) reset(cur cursors.IntegerArrayCursor) {
	c.IntegerArrayCursor = cur
	c.tmp.Timestamps, c.tmp.Values = nil, nil
}

func (c *integerArrayFilterCursor) Stats() cursors.CursorStats { return c.IntegerArrayCursor.Stats() }

func (c *integerArrayFilterCursor) Next() *cursors.IntegerArray {
	pos := 0
	c.res.Timestamps = c.res.Timestamps[:cap(c.res.Timestamps)]
	c.res.Values = c.res.Values[:cap(c.res.Values)]

	var a *cursors.IntegerArray

	if c.tmp.Len() > 0 {
		a = c.tmp
	} else {
		a = c.IntegerArrayCursor.Next()
	}

LOOP:
	for len(a.Timestamps) > 0 {
		for i, v := range a.Values {
			c.m.v = v
			if c.cond.EvalBool(c.m) {
				c.res.Timestamps[pos] = a.Timestamps[i]
				c.res.Values[pos] = v
				pos++
				if pos >= MaxPointsPerBlock {
					c.tmp.Timestamps = a.Timestamps[i+1:]
					c.tmp.Values = a.Values[i+1:]
					break LOOP
				}
			}
		}

		// Clear buffered timestamps & values if we make it through a cursor.
		// The break above will skip this if a cursor is partially read.
		c.tmp.Timestamps = nil
		c.tmp.Values = nil

		a = c.IntegerArrayCursor.Next()
	}

	c.res.Timestamps = c.res.Timestamps[:pos]
	c.res.Values = c.res.Values[:pos]

	return c.res
}

type integerMultiShardArrayCursor struct {
	cursors.IntegerArrayCursor
	cursorContext
	filter *integerArrayFilterCursor
}

func (c *integerMultiShardArrayCursor) reset(cur cursors.IntegerArrayCursor, itrs cursors.CursorIterators, cond expression) {
	if cond != nil {
		if c.filter == nil {
			c.filter = newIntegerFilterArrayCursor(cond)
		}
		c.filter.reset(cur)
		cur = c.filter
	}

	c.IntegerArrayCursor = cur
	c.itrs = itrs
	c.err = nil
}

func (c *integerMultiShardArrayCursor) Err() error { return c.err }

func (c *integerMultiShardArrayCursor) Stats() cursors.CursorStats {
	return c.IntegerArrayCursor.Stats()
}

func (c *integerMultiShardArrayCursor) Next() *cursors.IntegerArray {
	for {
		a := c.IntegerArrayCursor.Next()
		if a.Len() == 0 {
			if c.nextArrayCursor() {
				continue
			}
		}
		return a
	}
}

func (c *integerMultiShardArrayCursor) nextArrayCursor() bool {
	if len(c.itrs) == 0 {
		return false
	}

	c.IntegerArrayCursor.Close()

	var itr cursors.CursorIterator
	var cur cursors.Cursor
	for cur == nil && len(c.itrs) > 0 {
		itr, c.itrs = c.itrs[0], c.itrs[1:]
		cur, _ = itr.Next(c.ctx, c.req)
	}

	var ok bool
	if cur != nil {
		var next cursors.IntegerArrayCursor
		next, ok = cur.(cursors.IntegerArrayCursor)
		if !ok {
			cur.Close()
			next = IntegerEmptyArrayCursor
			c.err = errors.New("expected integer cursor")
		} else {
			if c.filter != nil {
				c.filter.reset(next)
				next = c.filter
			}
		}
		c.IntegerArrayCursor = next
	} else {
		c.IntegerArrayCursor = IntegerEmptyArrayCursor
	}

	return ok
}

type integerLimitArrayCursor struct {
	cursors.IntegerArrayCursor
	res  *cursors.IntegerArray
	done bool
}

func newIntegerLimitArrayCursor(cur cursors.IntegerArrayCursor) *integerLimitArrayCursor {
	return &integerLimitArrayCursor{
		IntegerArrayCursor: cur,
		res:                cursors.NewIntegerArrayLen(1),
	}
}

func (c *integerLimitArrayCursor) Stats() cursors.CursorStats { return c.IntegerArrayCursor.Stats() }

func (c *integerLimitArrayCursor) Next() *cursors.IntegerArray {
	if c.done {
		return &cursors.IntegerArray{}
	}
	a := c.IntegerArrayCursor.Next()
	if len(a.Timestamps) == 0 {
		return a
	}
	c.done = true
	c.res.Timestamps[0] = a.Timestamps[0]
	c.res.Values[0] = a.Values[0]
	return c.res
}

type integerEmptyArrayCursor struct {
	res cursors.IntegerArray
}

var IntegerEmptyArrayCursor cursors.IntegerArrayCursor = &integerEmptyArrayCursor{}

func (c *integerEmptyArrayCursor) Err() error                  { return nil }
func (c *integerEmptyArrayCursor) Close()                      {}
func (c *integerEmptyArrayCursor) Stats() cursors.CursorStats  { return cursors.CursorStats{} }
func (c *integerEmptyArrayCursor) Next() *cursors.IntegerArray { return &c.res }

// ********************
// Unsigned Array Cursor

type unsignedArrayFilterCursor struct {
	cursors.UnsignedArrayCursor
	cond expression
	m    *singleValue
	res  *cursors.UnsignedArray
	tmp  *cursors.UnsignedArray
}

func newUnsignedFilterArrayCursor(cond expression) *unsignedArrayFilterCursor {
	return &unsignedArrayFilterCursor{
		cond: cond,
		m:    &singleValue{},
		res:  cursors.NewUnsignedArrayLen(MaxPointsPerBlock),
		tmp:  &cursors.UnsignedArray{},
	}
}

func (c *unsignedArrayFilterCursor) reset(cur cursors.UnsignedArrayCursor) {
	c.UnsignedArrayCursor = cur
	c.tmp.Timestamps, c.tmp.Values = nil, nil
}

func (c *unsignedArrayFilterCursor) Stats() cursors.CursorStats { return c.UnsignedArrayCursor.Stats() }

func (c *unsignedArrayFilterCursor) Next() *cursors.UnsignedArray {
	pos := 0
	c.res.Timestamps = c.res.Timestamps[:cap(c.res.Timestamps)]
	c.res.Values = c.res.Values[:cap(c.res.Values)]

	var a *cursors.UnsignedArray

	if c.tmp.Len() > 0 {
		a = c.tmp
	} else {
		a = c.UnsignedArrayCursor.Next()
	}

LOOP:
	for len(a.Timestamps) > 0 {
		for i, v := range a.Values {
			c.m.v = v
			if c.cond.EvalBool(c.m) {
				c.res.Timestamps[pos] = a.Timestamps[i]
				c.res.Values[pos] = v
				pos++
				if pos >= MaxPointsPerBlock {
					c.tmp.Timestamps = a.Timestamps[i+1:]
					c.tmp.Values = a.Values[i+1:]
					break LOOP
				}
			}
		}

		// Clear buffered timestamps & values if we make it through a cursor.
		// The break above will skip this if a cursor is partially read.
		c.tmp.Timestamps = nil
		c.tmp.Values = nil

		a = c.UnsignedArrayCursor.Next()
	}

	c.res.Timestamps = c.res.Timestamps[:pos]
	c.res.Values = c.res.Values[:pos]

	return c.res
}

type unsignedMultiShardArrayCursor struct {
	cursors.UnsignedArrayCursor
	cursorContext
	filter *unsignedArrayFilterCursor
}

func (c *unsignedMultiShardArrayCursor) reset(cur cursors.UnsignedArrayCursor, itrs cursors.CursorIterators, cond expression) {
	if cond != nil {
		if c.filter == nil {
			c.filter = newUnsignedFilterArrayCursor(cond)
		}
		c.filter.reset(cur)
		cur = c.filter
	}

	c.UnsignedArrayCursor = cur
	c.itrs = itrs
	c.err = nil
}

func (c *unsignedMultiShardArrayCursor) Err() error { return c.err }

func (c *unsignedMultiShardArrayCursor) Stats() cursors.CursorStats {
	return c.UnsignedArrayCursor.Stats()
}

func (c *unsignedMultiShardArrayCursor) Next() *cursors.UnsignedArray {
	for {
		a := c.UnsignedArrayCursor.Next()
		if a.Len() == 0 {
			if c.nextArrayCursor() {
				continue
			}
		}
		return a
	}
}

func (c *unsignedMultiShardArrayCursor) nextArrayCursor() bool {
	if len(c.itrs) == 0 {
		return false
	}

	c.UnsignedArrayCursor.Close()

	var itr cursors.CursorIterator
	var cur cursors.Cursor
	for cur == nil && len(c.itrs) > 0 {
		itr, c.itrs = c.itrs[0], c.itrs[1:]
		cur, _ = itr.Next(c.ctx, c.req)
	}

	var ok bool
	if cur != nil {
		var next cursors.UnsignedArrayCursor
		next, ok = cur.(cursors.UnsignedArrayCursor)
		if !ok {
			cur.Close()
			next = UnsignedEmptyArrayCursor
			c.err = errors.New("expected unsigned cursor")
		} else {
			if c.filter != nil {
				c.filter.reset(next)
				next = c.filter
			}
		}
		c.UnsignedArrayCursor = next
	} else {
		c.UnsignedArrayCursor = UnsignedEmptyArrayCursor
	}

	return ok
}

type unsignedLimitArrayCursor struct {
	cursors.UnsignedArrayCursor
	res  *cursors.UnsignedArray
	done bool
}

func newUnsignedLimitArrayCursor(cur cursors.UnsignedArrayCursor) *unsignedLimitArrayCursor {
	return &unsignedLimitArrayCursor{
		UnsignedArrayCursor: cur,
		res:                 cursors.NewUnsignedArrayLen(1),
	}
}

func (c *unsignedLimitArrayCursor) Stats() cursors.CursorStats { return c.UnsignedArrayCursor.Stats() }

func (c *unsignedLimitArrayCursor) Next() *cursors.UnsignedArray {
	if c.done {
		return &cursors.UnsignedArray{}
	}
	a := c.UnsignedArrayCursor.Next()
	if len(a.Timestamps) == 0 {
		return a
	}
	c.done = true
	c.res.Timestamps[0] = a.Timestamps[0]
	c.res.Values[0] = a.Values[0]
	return c.res
}

type unsignedEmptyArrayCursor struct {
	res cursors.UnsignedArray
}

var UnsignedEmptyArrayCursor cursors.UnsignedArrayCursor = &unsignedEmptyArrayCursor{}

func (c *unsignedEmptyArrayCursor) Err() error                   { return nil }
func (c *unsignedEmptyArrayCursor) Close()                       {}
func (c *unsignedEmptyArrayCursor) Stats() cursors.CursorStats   { return cursors.CursorStats{} }
func (c *unsignedEmptyArrayCursor) Next() *cursors.UnsignedArray { return &c.res }

// ********************
// String Array Cursor

type stringArrayFilterCursor struct {
	cursors.StringArrayCursor
	cond expression
	m    *singleValue
	res  *cursors.StringArray
	tmp  *cursors.StringArray
}

func newStringFilterArrayCursor(cond expression) *stringArrayFilterCursor {
	return &stringArrayFilterCursor{
		cond: cond,
		m:    &singleValue{},
		res:  cursors.NewStringArrayLen(MaxPointsPerBlock),
		tmp:  &cursors.StringArray{},
	}
}

func (c *stringArrayFilterCursor) reset(cur cursors.StringArrayCursor) {
	c.StringArrayCursor = cur
	c.tmp.Timestamps, c.tmp.Values = nil, nil
}

func (c *stringArrayFilterCursor) Stats() cursors.CursorStats { return c.StringArrayCursor.Stats() }

func (c *stringArrayFilterCursor) Next() *cursors.StringArray {
	pos := 0
	c.res.Timestamps = c.res.Timestamps[:cap(c.res.Timestamps)]
	c.res.Values = c.res.Values[:cap(c.res.Values)]

	var a *cursors.StringArray

	if c.tmp.Len() > 0 {
		a = c.tmp
	} else {
		a = c.StringArrayCursor.Next()
	}

LOOP:
	for len(a.Timestamps) > 0 {
		for i, v := range a.Values {
			c.m.v = v
			if c.cond.EvalBool(c.m) {
				c.res.Timestamps[pos] = a.Timestamps[i]
				c.res.Values[pos] = v
				pos++
				if pos >= MaxPointsPerBlock {
					c.tmp.Timestamps = a.Timestamps[i+1:]
					c.tmp.Values = a.Values[i+1:]
					break LOOP
				}
			}
		}

		// Clear buffered timestamps & values if we make it through a cursor.
		// The break above will skip this if a cursor is partially read.
		c.tmp.Timestamps = nil
		c.tmp.Values = nil

		a = c.StringArrayCursor.Next()
	}

	c.res.Timestamps = c.res.Timestamps[:pos]
	c.res.Values = c.res.Values[:pos]

	return c.res
}

type stringMultiShardArrayCursor struct {
	cursors.StringArrayCursor
	cursorContext
	filter *stringArrayFilterCursor
}

func (c *stringMultiShardArrayCursor) reset(cur cursors.StringArrayCursor, itrs cursors.CursorIterators, cond expression) {
	if cond != nil {
		if c.filter == nil {
			c.filter = newStringFilterArrayCursor(cond)
		}
		c.filter.reset(cur)
		cur = c.filter
	}

	c.StringArrayCursor = cur
	c.itrs = itrs
	c.err = nil
}

func (c *stringMultiShardArrayCursor) Err() error { return c.err }

func (c *stringMultiShardArrayCursor) Stats() cursors.CursorStats {
	return c.StringArrayCursor.Stats()
}

func (c *stringMultiShardArrayCursor) Next() *cursors.StringArray {
	for {
		a := c.StringArrayCursor.Next()
		if a.Len() == 0 {
			if c.nextArrayCursor() {
				continue
			}
		}
		return a
	}
}

func (c *stringMultiShardArrayCursor) nextArrayCursor() bool {
	if len(c.itrs) == 0 {
		return false
	}

	c.StringArrayCursor.Close()

	var itr cursors.CursorIterator
	var cur cursors.Cursor
	for cur == nil && len(c.itrs) > 0 {
		itr, c.itrs = c.itrs[0], c.itrs[1:]
		cur, _ = itr.Next(c.ctx, c.req)
	}

	var ok bool
	if cur != nil {
		var next cursors.StringArrayCursor
		next, ok = cur.(cursors.StringArrayCursor)
		if !ok {
			cur.Close()
			next = StringEmptyArrayCursor
			c.err = errors.New("expected string cursor")
		} else {
			if c.filter != nil {
				c.filter.reset(next)
				next = c.filter
			}
		}
		c.StringArrayCursor = next
	} else {
		c.StringArrayCursor = StringEmptyArrayCursor
	}

	return ok
}

type stringLimitArrayCursor struct {
	cursors.StringArrayCursor
	res  *cursors.StringArray
	done bool
}

func newStringLimitArrayCursor(cur cursors.StringArrayCursor) *stringLimitArrayCursor {
	return &stringLimitArrayCursor{
		StringArrayCursor: cur,
		res:               cursors.NewStringArrayLen(1),
	}
}

func (c *stringLimitArrayCursor) Stats() cursors.CursorStats { return c.StringArrayCursor.Stats() }

func (c *stringLimitArrayCursor) Next() *cursors.StringArray {
	if c.done {
		return &cursors.StringArray{}
	}
	a := c.StringArrayCursor.Next()
	if len(a.Timestamps) == 0 {
		return a
	}
	c.done = true
	c.res.Timestamps[0] = a.Timestamps[0]
	c.res.Values[0] = a.Values[0]
	return c.res
}

type stringEmptyArrayCursor struct {
	res cursors.StringArray
}

var StringEmptyArrayCursor cursors.StringArrayCursor = &stringEmptyArrayCursor{}

func (c *stringEmptyArrayCursor) Err() error                 { return nil }
func (c *stringEmptyArrayCursor) Close()                     {}
func (c *stringEmptyArrayCursor) Stats() cursors.CursorStats { return cursors.CursorStats{} }
func (c *stringEmptyArrayCursor) Next() *cursors.StringArray { return &c.res }

// ********************
// Boolean Array Cursor

type booleanArrayFilterCursor struct {
	cursors.BooleanArrayCursor
	cond expression
	m    *singleValue
	res  *cursors.BooleanArray
	tmp  *cursors.BooleanArray
}

func newBooleanFilterArrayCursor(cond expression) *booleanArrayFilterCursor {
	return &booleanArrayFilterCursor{
		cond: cond,
		m:    &singleValue{},
		res:  cursors.NewBooleanArrayLen(MaxPointsPerBlock),
		tmp:  &cursors.BooleanArray{},
	}
}

func (c *booleanArrayFilterCursor) reset(cur cursors.BooleanArrayCursor) {
	c.BooleanArrayCursor = cur
	c.tmp.Timestamps, c.tmp.Values = nil, nil
}

func (c *booleanArrayFilterCursor) Stats() cursors.CursorStats { return c.BooleanArrayCursor.Stats() }

func (c *booleanArrayFilterCursor) Next() *cursors.BooleanArray {
	pos := 0
	c.res.Timestamps = c.res.Timestamps[:cap(c.res.Timestamps)]
	c.res.Values = c.res.Values[:cap(c.res.Values)]

	var a *cursors.BooleanArray

	if c.tmp.Len() > 0 {
		a = c.tmp
	} else {
		a = c.BooleanArrayCursor.Next()
	}

LOOP:
	for len(a.Timestamps) > 0 {
		for i, v := range a.Values {
			c.m.v = v
			if c.cond.EvalBool(c.m) {
				c.res.Timestamps[pos] = a.Timestamps[i]
				c.res.Values[pos] = v
				pos++
				if pos >= MaxPointsPerBlock {
					c.tmp.Timestamps = a.Timestamps[i+1:]
					c.tmp.Values = a.Values[i+1:]
					break LOOP
				}
			}
		}

		// Clear buffered timestamps & values if we make it through a cursor.
		// The break above will skip this if a cursor is partially read.
		c.tmp.Timestamps = nil
		c.tmp.Values = nil

		a = c.BooleanArrayCursor.Next()
	}

	c.res.Timestamps = c.res.Timestamps[:pos]
	c.res.Values = c.res.Values[:pos]

	return c.res
}

type booleanMultiShardArrayCursor struct {
	cursors.BooleanArrayCursor
	cursorContext
	filter *booleanArrayFilterCursor
}

func (c *booleanMultiShardArrayCursor) reset(cur cursors.BooleanArrayCursor, itrs cursors.CursorIterators, cond expression) {
	if cond != nil {
		if c.filter == nil {
			c.filter = newBooleanFilterArrayCursor(cond)
		}
		c.filter.reset(cur)
		cur = c.filter
	}

	c.BooleanArrayCursor = cur
	c.itrs = itrs
	c.err = nil
}

func (c *booleanMultiShardArrayCursor) Err() error { return c.err }

func (c *booleanMultiShardArrayCursor) Stats() cursors.CursorStats {
	return c.BooleanArrayCursor.Stats()
}

func (c *booleanMultiShardArrayCursor) Next() *cursors.BooleanArray {
	for {
		a := c.BooleanArrayCursor.Next()
		if a.Len() == 0 {
			if c.nextArrayCursor() {
				continue
			}
		}
		return a
	}
}

func (c *booleanMultiShardArrayCursor) nextArrayCursor() bool {
	if len(c.itrs) == 0 {
		return false
	}

	c.BooleanArrayCursor.Close()

	var itr cursors.CursorIterator
	var cur cursors.Cursor
	for cur == nil && len(c.itrs) > 0 {
		itr, c.itrs = c.itrs[0], c.itrs[1:]
		cur, _ = itr.Next(c.ctx, c.req)
	}

	var ok bool
	if cur != nil {
		var next cursors.BooleanArrayCursor
		next, ok = cur.(cursors.BooleanArrayCursor)
		if !ok {
			cur.Close()
			next = BooleanEmptyArrayCursor
			c.err = errors.New("expected boolean cursor")
		} else {
			if c.filter != nil {
				c.filter.reset(next)
				next = c.filter
			}
		}
		c.BooleanArrayCursor = next
	} else {
		c.BooleanArrayCursor = BooleanEmptyArrayCursor
	}

	return ok
}

type booleanLimitArrayCursor struct {
	cursors.BooleanArrayCursor
	res  *cursors.BooleanArray
	done bool
}

func newBooleanLimitArrayCursor(cur cursors.BooleanArrayCursor) *booleanLimitArrayCursor {
	return &booleanLimitArrayCursor{
		BooleanArrayCursor: cur,
		res:                cursors.NewBooleanArrayLen(1),
	}
}

func (c *booleanLimitArrayCursor) Stats() cursors.CursorStats { return c.BooleanArrayCursor.Stats() }

func (c *booleanLimitArrayCursor) Next() *cursors.BooleanArray {
	if c.done {
		return &cursors.BooleanArray{}
	}
	a := c.BooleanArrayCursor.Next()
	if len(a.Timestamps) == 0 {
		return a
	}
	c.done = true
	c.res.Timestamps[0] = a.Timestamps[0]
	c.res.Values[0] = a.Values[0]
	return c.res
}

type booleanEmptyArrayCursor struct {
	res cursors.BooleanArray
}

var BooleanEmptyArrayCursor cursors.BooleanArrayCursor = &booleanEmptyArrayCursor{}

func (c *booleanEmptyArrayCursor) Err() error                  { return nil }
func (c *booleanEmptyArrayCursor) Close()                      {}
func (c *booleanEmptyArrayCursor) Stats() cursors.CursorStats  { return cursors.CursorStats{} }
func (c *booleanEmptyArrayCursor) Next() *cursors.BooleanArray { return &c.res }

func arrayCursorType(cur cursors.Cursor) string {
	switch cur.(type) {

	case cursors.FloatArrayCursor:
		return "float"

	case cursors.IntegerArrayCursor:
		return "integer"

	case cursors.UnsignedArrayCursor:
		return "unsigned"

	case cursors.StringArrayCursor:
		return "string"

	case cursors.BooleanArrayCursor:
		return "boolean"

	default:
		return "unknown"
	}
}
