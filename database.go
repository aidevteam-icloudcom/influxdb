package influxdb

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/influxdb/influxdb/influxql"
)

// database is a collection of retention policies and shards. It also has methods
// for keeping an in memory index of all the measurements, series, and tags in the database.
// Methods on this struct aren't goroutine safe. They assume that the server is handling
// any locking to make things safe.
type database struct {
	name string

	policies map[string]*RetentionPolicy // retention policies by name

	defaultRetentionPolicy string

	// in memory indexing structures
	measurements map[string]*Measurement // measurement name to object and index
	series       map[uint32]*Series      // map series id to the Series object
	names        []string                // sorted list of the measurement names
}

// newDatabase returns an instance of database.
func newDatabase() *database {
	return &database{
		policies:     make(map[string]*RetentionPolicy),
		measurements: make(map[string]*Measurement),
		series:       make(map[uint32]*Series),
		names:        make([]string, 0),
	}
}

// shardGroupByTimestamp returns a shard group that owns a given timestamp.
func (db *database) shardGroupByTimestamp(policy string, timestamp time.Time) (*ShardGroup, error) {
	p := db.policies[policy]
	if p == nil {
		return nil, ErrRetentionPolicyNotFound
	}
	return p.shardGroupByTimestamp(timestamp), nil
}

// seriesIDs returns an array of series ids for the given measurements and filters to be applied to all.
// Filters are equivalent to an AND operation. If you want to do an OR, get the series IDs for one set,
// then get the series IDs for another set and use the SeriesIDs.Union to combine the two.
func (d *database) seriesIDs(names []string, filters []*TagFilter) seriesIDs {
	// they want all ids if no filters are specified
	if len(filters) == 0 {
		ids := seriesIDs(make([]uint32, 0))
		for _, m := range d.measurements {
			ids = ids.union(m.seriesIDs)
		}
		return ids
	}

	ids := seriesIDs(make([]uint32, 0))
	for _, n := range names {
		ids = ids.union(d.seriesIDsByName(n, filters))
	}

	return ids
}

// MeasurementNames returns a list of measurement names.
func (d *database) MeasurementNames() []string {
	names := make([]string, 0, len(d.measurements))
	for k, _ := range d.measurements {
		names = append(names, k)
	}
	return names
}

// Series takes a series ID and returns a series.
func (d *database) Series(id uint32) *Series {
	return d.series[id]
}

// TagKeys returns a sorted array of unique tag keys for the given measurements.
// If an empty or nil slice is passed in, the tag keys for the entire database will be returned.
func (d *database) TagKeys(names []string) []string {
	if len(names) == 0 {
		names = d.names
	}

	keys := make(map[string]bool)
	for _, n := range names {
		idx := d.measurements[n]
		if idx != nil {
			for k, _ := range idx.seriesByTagKeyValue {
				keys[k] = true
			}
		}
	}

	sortedKeys := make([]string, 0, len(keys))
	for k, _ := range keys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	return sortedKeys
}

// TagValues returns a map of unique tag values for the given measurements and key with the given filters applied.
// Call .ToSlice() on the result to convert it into a sorted slice of strings.
// Filters are equivalent to and AND operation. If you want to do an OR, get the tag values for one set,
// then get the tag values for another set and do a union of the two.
func (d *database) TagValues(names []string, key string, filters []*TagFilter) TagValues {
	values := TagValues(make(map[string]bool))

	// see if they just want all the tag values for this key
	if len(filters) == 0 {
		for _, n := range names {
			idx := d.measurements[n]
			if idx != nil {
				values.Union(idx.tagValues(key))
			}
		}
		return values
	}

	// they have filters so just get a set of series ids matching them and then get the tag values from those
	seriesIDs := d.seriesIDs(names, filters)
	return d.tagValuesBySeries(key, seriesIDs)
}

// tagValuesBySeries will return a TagValues map of all the unique tag values for a collection of series.
func (d *database) tagValuesBySeries(key string, ids seriesIDs) TagValues {
	values := make(map[string]bool)
	for _, id := range ids {
		s := d.series[id]
		if s == nil {
			continue
		}
		if v, ok := s.Tags[key]; ok {
			values[v] = true
		}
	}
	return TagValues(values)
}

type TagValues map[string]bool

// Slice returns a sorted slice of the TagValues
func (t TagValues) Slice() []string {
	a := make([]string, 0, len(t))
	for v, _ := range t {
		a = append(a, v)
	}
	sort.Strings(a)
	return a
}

// Union will modify the receiver by merging in the passed in values.
func (l TagValues) Union(r TagValues) {
	for v, _ := range r {
		l[v] = true
	}
}

// Intersect will modify the receiver by keeping only the keys that exist in the passed in values
func (l TagValues) Intersect(r TagValues) {
	for v, _ := range l {
		if _, ok := r[v]; !ok {
			delete(l, v)
		}
	}
}

// seriesIDsByName is the same as SeriesIDs, but for a specific measurement.
func (d *database) seriesIDsByName(name string, filters []*TagFilter) seriesIDs {
	m := d.measurements[name]
	if m == nil {
		return nil
	}

	// process the filters one at a time to get the list of ids they return
	idsPerFilter := make([]seriesIDs, len(filters), len(filters))
	for i, filter := range filters {
		idsPerFilter[i] = m.seriesIDsByFilter(filter)
	}

	// collapse the set of ids
	allIDs := idsPerFilter[0]
	for i := 1; i < len(filters); i++ {
		allIDs = allIDs.intersect(idsPerFilter[i])
	}

	return allIDs
}

// MarshalJSON encodes a database into a JSON-encoded byte slice.
func (db *database) MarshalJSON() ([]byte, error) {
	// Copy over properties to intermediate type.
	var o databaseJSON
	o.Name = db.name
	o.DefaultRetentionPolicy = db.defaultRetentionPolicy
	for _, rp := range db.policies {
		o.Policies = append(o.Policies, rp)
	}
	return json.Marshal(&o)
}

// UnmarshalJSON decodes a JSON-encoded byte slice to a database.
func (db *database) UnmarshalJSON(data []byte) error {
	// Decode into intermediate type.
	var o databaseJSON
	if err := json.Unmarshal(data, &o); err != nil {
		return err
	}

	// Copy over properties from intermediate type.
	db.name = o.Name
	db.defaultRetentionPolicy = o.DefaultRetentionPolicy

	// Copy shard policies.
	db.policies = make(map[string]*RetentionPolicy)
	for _, rp := range o.Policies {
		db.policies[rp.Name] = rp
	}

	return nil
}

// databaseJSON represents the JSON-serialization format for a database.
type databaseJSON struct {
	Name                   string             `json:"name,omitempty"`
	DefaultRetentionPolicy string             `json:"defaultRetentionPolicy,omitempty"`
	Policies               []*RetentionPolicy `json:"policies,omitempty"`
}

// Measurement represents a collection of time series in a database. It also contains in memory
// structures for indexing tags. These structures are accessed through private methods on the Measurement
// object. Generally these methods are only accessed from Index, which is responsible for ensuring
// go routine safe access.
type Measurement struct {
	Name   string   `json:"name,omitempty"`
	Fields []*Field `json:"fields,omitempty"`

	// in-memory index fields
	series              map[string]*Series // sorted tagset string to the series object
	seriesByID          map[uint32]*Series // lookup table for series by their id
	measurement         *Measurement
	seriesByTagKeyValue map[string]map[string]seriesIDs // map from tag key to value to sorted set of series ids
	seriesIDs           seriesIDs                       // sorted list of series IDs in this measurement
}

func NewMeasurement(name string) *Measurement {
	return &Measurement{
		Name:   name,
		Fields: make([]*Field, 0),

		series:              make(map[string]*Series),
		seriesByID:          make(map[uint32]*Series),
		seriesByTagKeyValue: make(map[string]map[string]seriesIDs),
		seriesIDs:           make(seriesIDs, 0),
	}
}

// createFieldIfNotExists creates a new field with an autoincrementing ID.
// Returns an error if 255 fields have already been created on the measurement.
func (m *Measurement) createFieldIfNotExists(name string, typ influxql.DataType) (*Field, error) {
	// Ignore if the field already exists.
	if f := m.FieldByName(name); f != nil {
		return f, nil
	}

	// Only 255 fields are allowed. If we go over that then return an error.
	if len(m.Fields)+1 > math.MaxUint8 {
		return nil, ErrFieldOverflow
	}

	// Create and append a new field.
	f := &Field{
		ID:   uint8(len(m.Fields) + 1),
		Name: name,
		Type: typ,
	}
	m.Fields = append(m.Fields, f)

	return f, nil
}

// Field returns a field by id.
func (m *Measurement) Field(id uint8) *Field {
	for _, f := range m.Fields {
		if f.ID == id {
			return f
		}
	}
	return nil
}

// FieldByName returns a field by name.
func (m *Measurement) FieldByName(name string) *Field {
	for _, f := range m.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// addSeries will add a series to the measurementIndex. Returns false if already present
func (m *Measurement) addSeries(s *Series) bool {
	if _, ok := m.seriesByID[s.ID]; ok {
		return false
	}
	m.seriesByID[s.ID] = s
	tagset := string(marshalTags(s.Tags))
	m.series[tagset] = s
	m.seriesIDs = append(m.seriesIDs, s.ID)

	// the series ID should always be higher than all others because it's a new
	// series. So don't do the sort if we don't have to.
	if len(m.seriesIDs) > 1 && m.seriesIDs[len(m.seriesIDs)-1] < m.seriesIDs[len(m.seriesIDs)-2] {
		sort.Sort(m.seriesIDs)
	}

	// add this series id to the tag index on the measurement
	for k, v := range s.Tags {
		valueMap := m.seriesByTagKeyValue[k]
		if valueMap == nil {
			valueMap = make(map[string]seriesIDs)
			m.seriesByTagKeyValue[k] = valueMap
		}
		ids := valueMap[v]
		ids = append(ids, s.ID)

		// most of the time the series ID will be higher than all others because it's a new
		// series. So don't do the sort if we don't have to.
		if len(ids) > 1 && ids[len(ids)-1] < ids[len(ids)-2] {
			sort.Sort(ids)
		}
		valueMap[v] = ids
	}

	return true
}

// tagValues returns a map of unique tag values for the given key
func (m *Measurement) tagValues(key string) TagValues {
	tags := m.seriesByTagKeyValue[key]
	values := make(map[string]bool, len(tags))
	for k, _ := range tags {
		values[k] = true
	}
	return TagValues(values)
}

// seriesByTags returns the Series that matches the given tagset.
func (m *Measurement) seriesByTags(tags map[string]string) *Series {
	return m.series[string(marshalTags(tags))]
}

// seriesIDs returns the series ids for a given filter
func (m *Measurement) seriesIDsByFilter(filter *TagFilter) (ids seriesIDs) {
	values := m.seriesByTagKeyValue[filter.Key]
	if values == nil {
		return
	}

	// handle regex filters
	if filter.Regex != nil {
		for k, v := range values {
			if filter.Regex.MatchString(k) {
				if ids == nil {
					ids = v
				} else {
					ids = ids.union(v)
				}
			}
		}
		if filter.Not {
			ids = m.seriesIDs.reject(ids)
		}
		return
	}

	// this is for the value is not null query
	if filter.Not && filter.Value == "" {
		for _, v := range values {
			if ids == nil {
				ids = v
			} else {
				ids.intersect(v)
			}
		}
		return
	}

	// get the ids that have the given key/value tag pair
	ids = seriesIDs(values[filter.Value])

	// filter out these ids from the entire set if it's a not query
	if filter.Not {
		ids = m.seriesIDs.reject(ids)
	}

	return
}

// mapValues converts a map of values with string keys to field id keys.
// Returns nil if any field doesn't exist.
func (m *Measurement) mapValues(values map[string]interface{}) map[uint8]interface{} {
	other := make(map[uint8]interface{}, len(values))
	for k, v := range values {
		// TODO: Cast value to original field type.

		f := m.FieldByName(k)
		if f == nil {
			return nil
		}
		other[f.ID] = v
	}
	return other
}

// expandExpr returns a list of expressions expanded by all possible tag combinations.
func (m *Measurement) expandExpr(expr influxql.Expr) []tagSetExpr {
	// Retrieve list of unique values for each tag.
	valuesByTagKey := m.uniqueTagValues(expr)

	// Convert keys to slices.
	keys := make([]string, 0, len(valuesByTagKey))
	for key := range valuesByTagKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Order uniques by key.
	uniques := make([][]string, len(keys))
	for i, key := range keys {
		uniques[i] = valuesByTagKey[key]
	}

	// Reduce a condition for each combination of tag values.
	return expandExprWithValues(expr, keys, []tagExpr{}, uniques, 0)
}

func expandExprWithValues(expr influxql.Expr, keys []string, tagExprs []tagExpr, uniques [][]string, index int) []tagSetExpr {
	// If we have no more keys left then execute the reduction and return.
	if index == len(keys) {
		// Create a map of tag key/values.
		m := make(map[string]*string, len(keys))
		for i, key := range keys {
			if tagExprs[i].op == influxql.EQ {
				m[key] = &tagExprs[i].values[0]
			} else {
				m[key] = nil
			}
		}

		// TODO: Rewrite full expressions instead of VarRef replacement.

		// Reduce using the current tag key/value set.
		// Ignore it if reduces down to "false".
		e := influxql.Reduce(expr, &tagValuer{tags: m})
		if e, ok := e.(*influxql.BooleanLiteral); ok && e.Val == false {
			return nil
		}

		return []tagSetExpr{{values: copyTagExprs(tagExprs), expr: e}}
	}

	// Otherwise expand for each possible equality value of the key.
	var exprs []tagSetExpr
	for _, v := range uniques[index] {
		exprs = append(exprs, expandExprWithValues(expr, keys, append(tagExprs, tagExpr{keys[index], []string{v}, influxql.EQ}), uniques, index+1)...)
	}
	exprs = append(exprs, expandExprWithValues(expr, keys, append(tagExprs, tagExpr{keys[index], uniques[index], influxql.NEQ}), uniques, index+1)...)

	return exprs
}

// tagValuer is used during expression expansion to evaluate all sets of tag values.
type tagValuer struct {
	tags map[string]*string
}

// Value returns the string value of a tag and true if it's listed in the tagset.
func (v *tagValuer) Value(name string) (interface{}, bool) {
	if value, ok := v.tags[name]; ok {
		if value == nil {
			return nil, true
		}
		return *value, true
	}
	return nil, false
}

// tagSetExpr represents a set of tag keys/values and associated expression.
type tagSetExpr struct {
	values []tagExpr
	expr   influxql.Expr
}

// tagExpr represents one or more values assigned to a given tag.
type tagExpr struct {
	key    string
	values []string
	op     influxql.Token // EQ or NEQ
}

func copyTagExprs(a []tagExpr) []tagExpr {
	other := make([]tagExpr, len(a))
	copy(other, a)
	return other
}

// uniqueTagValues returns a list of unique tag values used in an expression.
func (m *Measurement) uniqueTagValues(expr influxql.Expr) map[string][]string {
	// Track unique value per tag.
	tags := make(map[string]map[string]struct{})

	// Find all tag values referenced in the expression.
	influxql.WalkFunc(expr, func(n influxql.Node) {
		switch n := n.(type) {
		case *influxql.BinaryExpr:
			// Ignore operators that are not equality.
			if n.Op != influxql.EQ {
				return
			}

			// Extract ref and string literal.
			var key, value string
			switch lhs := n.LHS.(type) {
			case *influxql.VarRef:
				if rhs, ok := n.RHS.(*influxql.StringLiteral); ok {
					key, value = lhs.Val, rhs.Val
				}
			case *influxql.StringLiteral:
				if rhs, ok := n.RHS.(*influxql.VarRef); ok {
					key, value = rhs.Val, lhs.Val
				}
			}
			if key == "" {
				return
			}

			// Add value to set.
			if tags[key] == nil {
				tags[key] = make(map[string]struct{})
			}
			tags[key][value] = struct{}{}
		}
	})

	// Convert to map of slices.
	out := make(map[string][]string)
	for k, values := range tags {
		out[k] = make([]string, 0, len(values))
		for v := range values {
			out[k] = append(out[k], v)
		}
		sort.Strings(out[k])
	}
	return out
}

type Measurements []*Measurement

// Field represents a series field.
type Field struct {
	ID   uint8             `json:"id,omitempty"`
	Name string            `json:"name,omitempty"`
	Type influxql.DataType `json:"type,omitempty"`
}

// Fields represents a list of fields.
type Fields []*Field

// Series belong to a Measurement and represent unique time series in a database
type Series struct {
	ID   uint32
	Tags map[string]string

	measurement *Measurement
}

// match returns true if all tags match the series' tags.
func (s *Series) match(tags map[string]string) bool {
	for k, v := range tags {
		if s.Tags[k] != v {
			return false
		}
	}
	return true
}

// TagFilter represents a tag filter when looking up other tags or measurements.
type TagFilter struct {
	Not   bool
	Key   string
	Value string
	Regex *regexp.Regexp
}

// seriesIDs is a convenience type for sorting, checking equality, and doing
// union and intersection of collections of series ids.
type seriesIDs []uint32

func (p seriesIDs) Len() int           { return len(p) }
func (p seriesIDs) Less(i, j int) bool { return p[i] < p[j] }
func (p seriesIDs) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// equals assumes that both are sorted.
func (a seriesIDs) equals(other seriesIDs) bool {
	if len(a) != len(other) {
		return false
	}
	for i, s := range other {
		if a[i] != s {
			return false
		}
	}
	return true
}

// intersect returns a new collection of series ids in sorted order that is the intersection of the two.
// The two collections must already be sorted.
func (a seriesIDs) intersect(other seriesIDs) seriesIDs {
	l := a
	r := other

	// we want to iterate through the shortest one and stop
	if len(other) < len(a) {
		l = other
		r = a
	}

	// they're in sorted order so advance the counter as needed.
	// That is, don't run comparisons against lower values that we've already passed
	var i, j int

	ids := make([]uint32, 0, len(l))
	for i < len(l) && j < len(r) {
		if l[i] == r[j] {
			ids = append(ids, l[i])
			i += 1
			j += 1
		} else if l[i] < r[j] {
			i += 1
		} else {
			j += 1
		}
	}

	return seriesIDs(ids)
}

// union returns a new collection of series ids in sorted order that is the union of the two.
// The two collections must already be sorted.
func (l seriesIDs) union(r seriesIDs) seriesIDs {
	ids := make([]uint32, 0, len(l)+len(r))
	var i, j int
	for i < len(l) && j < len(r) {
		if l[i] == r[j] {
			ids = append(ids, l[i])
			i += 1
			j += 1
		} else if l[i] < r[j] {
			ids = append(ids, l[i])
			i += 1
		} else {
			ids = append(ids, r[j])
			j += 1
		}
	}

	// now append the remainder
	if i < len(l) {
		ids = append(ids, l[i:]...)
	} else if j < len(r) {
		ids = append(ids, r[j:]...)
	}

	return ids
}

// reject returns a new collection of series ids in sorted order with the passed in set removed from the original.
// This is useful for the NOT operator. The two collections must already be sorted.
func (l seriesIDs) reject(r seriesIDs) seriesIDs {
	var i, j int

	ids := make([]uint32, 0, len(l))
	for i < len(l) && j < len(r) {
		if l[i] == r[j] {
			i += 1
			j += 1
		} else if l[i] < r[j] {
			ids = append(ids, l[i])
			i += 1
		} else {
			j += 1
		}
	}

	// Append the remainder
	if i < len(l) {
		ids = append(ids, l[i:]...)
	}

	return seriesIDs(ids)
}

// RetentionPolicy represents a policy for creating new shards in a database and how long they're kept around for.
type RetentionPolicy struct {
	// Unique name within database. Required.
	Name string

	// Length of time to keep data around
	Duration time.Duration

	// The number of copies to make of each shard.
	ReplicaN uint32

	shardGroups []*ShardGroup
}

// NewRetentionPolicy returns a new instance of RetentionPolicy with defaults set.
func NewRetentionPolicy(name string) *RetentionPolicy {
	return &RetentionPolicy{
		Name:     name,
		ReplicaN: DefaultReplicaN,
		Duration: DefaultShardRetention,
	}
}

// shardGroupByTimestamp returns the group in the policy that owns a timestamp.
// Returns nil group does not exist.
func (rp *RetentionPolicy) shardGroupByTimestamp(timestamp time.Time) *ShardGroup {
	for _, g := range rp.shardGroups {
		if timeBetweenInclusive(timestamp, g.StartTime, g.EndTime) {
			return g
		}
	}
	return nil
}

// MarshalJSON encodes a retention policy to a JSON-encoded byte slice.
func (rp *RetentionPolicy) MarshalJSON() ([]byte, error) {
	var o retentionPolicyJSON
	o.Name = rp.Name
	o.Duration = rp.Duration
	o.ReplicaN = rp.ReplicaN
	for _, g := range rp.shardGroups {
		o.ShardGroups = append(o.ShardGroups, g)
	}
	return json.Marshal(&o)
}

// UnmarshalJSON decodes a JSON-encoded byte slice to a retention policy.
func (rp *RetentionPolicy) UnmarshalJSON(data []byte) error {
	// Decode into intermediate type.
	var o retentionPolicyJSON
	if err := json.Unmarshal(data, &o); err != nil {
		return err
	}

	// Copy over properties from intermediate type.
	rp.Name = o.Name
	rp.ReplicaN = o.ReplicaN
	rp.Duration = o.Duration
	rp.shardGroups = o.ShardGroups

	return nil
}

// retentionPolicyJSON represents an intermediate struct for JSON marshaling.
type retentionPolicyJSON struct {
	Name        string        `json:"name"`
	ReplicaN    uint32        `json:"replicaN,omitempty"`
	SplitN      uint32        `json:"splitN,omitempty"`
	Duration    time.Duration `json:"duration,omitempty"`
	ShardGroups []*ShardGroup `json:"shardGroups,omitempty"`
}

// addSeriesToIndex adds the series for the given measurement to the index.
// Returns true if not already present.
func (d *database) addSeriesToIndex(measurementName string, s *Series) bool {
	// if there is a measurement for this id, it's already been added
	if d.series[s.ID] != nil {
		return false
	}

	// get or create the measurement index and index it globally and in the measurement
	idx := d.createMeasurementIfNotExists(measurementName)

	s.measurement = idx
	d.series[s.ID] = s

	// TODO: add this series to the global tag index

	return idx.addSeries(s)
}

// createMeasurementIfNotExists will either add a measurement object to the index or return the existing one.
func (d *database) createMeasurementIfNotExists(name string) *Measurement {
	idx := d.measurements[name]
	if idx == nil {
		idx = NewMeasurement(name)
		d.measurements[name] = idx
		d.names = append(d.names, name)
		sort.Strings(d.names)
	}
	return idx
}

// MeasurementAndSeries returns the Measurement and the Series for a given measurement name and tag set.
func (d *database) MeasurementAndSeries(name string, tags map[string]string) (*Measurement, *Series) {
	idx := d.measurements[name]
	if idx == nil {
		return nil, nil
	}
	return idx, idx.seriesByTags(tags)
}

// used to convert the tag set to bytes for use as a lookup key
func marshalTags(tags map[string]string) []byte {
	s := make([]string, 0, len(tags))
	// pull out keys to sort
	for k := range tags {
		s = append(s, k)
	}
	sort.Strings(s)

	// now append on the key values in key sorted order
	for _, k := range s {
		s = append(s, tags[k])
	}
	return []byte(strings.Join(s, "|"))
}

// timeBetweenInclusive returns true if t is between min and max, inclusive.
func timeBetweenInclusive(t, min, max time.Time) bool {
	return (t.Equal(min) || t.After(min)) && (t.Equal(max) || t.Before(max))
}

// seriesIDsByExpr given a measurement and expression containing
// only tags returns a list of series IDs.
func (d *database) seriesIDsByExpr(measurements []string, expr influxql.Expr) (seriesIDs, error) {
	switch e := expr.(type) {
	case *influxql.BinaryExpr:
		switch e.Op {
		case influxql.EQ, influxql.NEQ:
			tag, ok := e.LHS.(*influxql.VarRef)
			if !ok {
				return nil, fmt.Errorf("left side of '=' must be a tag name")
			}

			value, ok := e.RHS.(*influxql.StringLiteral)
			if !ok {
				return nil, fmt.Errorf("right side of '=' must be a tag value string")
			}

			tf := &TagFilter{
				Not:   e.Op == influxql.NEQ,
				Key:   tag.Val,
				Value: value.Val,
			}
			return d.seriesIDs(measurements, []*TagFilter{tf}), nil
		case influxql.OR, influxql.AND:
			lhsIDs, err := d.seriesIDsByExpr(measurements, e.LHS)
			if err != nil {
				return nil, err
			}

			rhsIDs, err := d.seriesIDsByExpr(measurements, e.RHS)
			if err != nil {
				return nil, err
			}

			if e.Op == influxql.OR {
				return lhsIDs.union(rhsIDs), nil
			} else {
				return lhsIDs.intersect(rhsIDs), nil
			}
		default:
			return nil, fmt.Errorf("invalid operator")
		}
	case *influxql.ParenExpr:
		return d.seriesIDsByExpr(measurements, e.Expr)
	}
	return nil, fmt.Errorf("%#v", expr)
}
