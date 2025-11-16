// Copyright (c) 2024 RoseLoverX

package tl

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"errors"
)

const tagName = "tl"

type fieldTag struct {
	index            int  // flags:<N>
	encodedInBitflag bool // encoded_in_bitflags
	ignore           bool // -
	optional         bool // omitempty
	version          int  // flags||flags2
}

func parseTag(s reflect.StructTag) (*fieldTag, error) {
	tags, err := parseFunc(string(s))
	if err != nil {
		return nil, fmt.Errorf("parsing field tags: %w", err)
	}

	tag, err := tags.Get(tagName)
	if err != nil {
		return nil, nil
	}

	info := &fieldTag{}

	if tag.Name == "-" {
		info.ignore = true
		return info, nil
	}

	var flagIndexSet bool
	if strings.HasPrefix(tag.Name, "flag2:") {
		num := strings.TrimPrefix(tag.Name, "flag2:")
		index, err := parseUintMax32(num)
		info.index = int(index)
		if err != nil {
			return nil, fmt.Errorf("parsing index number '%s': %w", num, err)
		}

		info.optional = true

		flagIndexSet = true
		info.version = 2
	} else if strings.HasPrefix(tag.Name, "flag:") {
		num := strings.TrimPrefix(tag.Name, "flag:")
		index, err := parseUintMax32(num)
		info.index = int(index)
		if err != nil {
			return nil, fmt.Errorf("parsing index number '%s': %w", num, err)
		}

		info.optional = true
		info.version = 1

		flagIndexSet = true
	}

	if haveInSlice("encoded_in_bitflags", tag.Options) {
		if !flagIndexSet {
			return nil, errors.New("have 'encoded_in_bitflag' option without flag index")
		}

		info.encodedInBitflag = true
	}

	if haveInSlice("omitempty", tag.Options) {
		info.optional = true
	}

	return info, nil
}

// ! slicetricks
func haveInSlice(s string, slice []string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}

	return false
}

var (
	errTagSyntax      = errors.New("bad syntax for struct tag pair")
	errTagKeySyntax   = errors.New("bad syntax for struct tag key")
	errTagValueSyntax = errors.New("bad syntax for struct tag value")

	errKeyNotSet   = errors.New("tag key does not exist")
	errTagNotExist = errors.New("tag does not exist")
)

// Tags represent a set of tags from a single struct field
type Tags struct {
	tags []*Tag
}

// Tag defines a single struct's string literal tag
type Tag struct {
	// Key is the tag key, such as json, xml, etc..
	// i.e: `json:"foo,omitempty". Here key is: "json"
	Key string

	// Name is a part of the value
	// i.e: `json:"foo,omitempty". Here name is: "foo"
	Name string

	// Options is a part of the value. It contains a slice of tag options i.e:
	// `json:"foo,omitempty". Here options is: ["omitempty"]
	Options []string
}

// Parse parses a single struct field tag and returns the set of tags.
func parseFunc(tag string) (*Tags, error) {
	var tags []*Tag

	hasTag := tag != ""

	// NOTE(arslan) following code is from reflect and vet package with some
	// modifications to collect all necessary information and extend it with
	// usable methods
	for tag != "" {
		// Skip leading space.
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}

		// Scan to colon. A space, a quote or a control character is a syntax
		// error. Strictly speaking, control chars include the range [0x7f,
		// 0x9f], not just [0x00, 0x1f], but in practice, we ignore the
		// multi-byte control characters as it is simpler to inspect the tag's
		// bytes than the tag's runes.
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
			i++
		}

		if i == 0 {
			return nil, errTagKeySyntax
		}
		if i+1 >= len(tag) || tag[i] != ':' {
			return nil, errTagSyntax
		}
		if tag[i+1] != '"' {
			return nil, errTagValueSyntax
		}

		key := string(tag[:i])
		tag = tag[i+1:]

		// Scan quoted string to find value.
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			return nil, errTagValueSyntax
		}

		qvalue := string(tag[:i+1])
		tag = tag[i+1:]

		value, err := strconv.Unquote(qvalue)
		if err != nil {
			return nil, errTagValueSyntax
		}

		res := strings.Split(value, ",")
		name := res[0]
		options := res[1:]
		if len(options) == 0 {
			options = nil
		}

		tags = append(tags, &Tag{
			Key:     key,
			Name:    name,
			Options: options,
		})
	}

	if hasTag && len(tags) == 0 {
		return nil, nil
	}

	return &Tags{
		tags: tags,
	}, nil
}

// Get returns the tag associated with the given key. If the key is present
// in the tag the value (which may be empty) is returned. Otherwise the
// returned value will be the empty string. The ok return value reports whether
// the tag exists or not (which the return value is nil).
func (t *Tags) Get(key string) (*Tag, error) {
	for _, tag := range t.tags {
		if tag.Key == key {
			return tag, nil
		}
	}

	return nil, errTagNotExist
}

// Set sets the given tag. If the tag key already exists it'll override it
func (t *Tags) Set(tag *Tag) error {
	if tag.Key == "" {
		return errKeyNotSet
	}

	added := false
	for i, tg := range t.tags {
		if tg.Key == tag.Key {
			added = true
			t.tags[i] = tag
		}
	}

	if !added {
		// this means this is a new tag, add it
		t.tags = append(t.tags, tag)
	}

	return nil
}

const (
	bit32       = 5  // 5 bits to make 32 different variants
	defaultBase = 10 // base 10 of numbers
)

func parseUintMax32(s string) (uint8, error) {
	if pos, err := strconv.ParseUint(s, defaultBase, bit32); err == nil {
		return uint8(pos), nil
	}

	return 0, fmt.Errorf("invalid uint32 value: %s", s)
}
