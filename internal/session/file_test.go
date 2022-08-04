// Copyright (c) 2020-2021 KHS Films
//
// This file is a part of mtproto package.
// See https://github.com/amarnathcjd/gogram/blob/master/LICENSE for details

package session_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/amarnathcjd/gogram/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMTProto_SaveSession(t *testing.T) {
	storePath := filepath.Join(os.TempDir(), "session.json")
	defer os.Remove(storePath)

	os.Remove(storePath)

	storage := session.NewFromFile(storePath)
	err := storage.Store(&session.Session{
		Key:      []byte("some auth key"),
		Hash:     []byte("oooooh that's definitely a key hash!"),
		Salt:     0,
		Hostname: "1337.228.1488.0",
	})
	assert.NoError(t, err)

	data, err := ioutil.ReadFile(storePath)
	check(err)

	assert.Equal(t, `{"key":"c29tZSBhdXRoIGtleQ==","hash":"b29vb29oIHRoYXQncyBkZWZpbml0ZWx5IGEga2V5IGhhc2gh"`+
		`,"salt":"AAAAAAAAAAA=","hostname":"1337.228.1488.0"}`, string(data))
}

func TestMTProto_LoadSession(t *testing.T) {
	storePath := filepath.Join(os.TempDir(), "session.json")
	tmpData := `{"key":"c29tZSBhdXRoIGtleQ==","hash":"b29vb29oIHRoYXQncyBkZWZpbml0ZWx5IGEga2V5IGhhc2gh"` +
		`,"salt":"AAAAAAAAAAA=","hostname":"1337.228.1488.0"}`
	ioutil.WriteFile(storePath, []byte(tmpData), 0666)
	defer os.Remove(storePath)

	storage := session.NewFromFile(storePath)

	sess, err := storage.Load()
	require.NoError(t, err)

	assert.Equal(t, &session.Session{
		Key:      []byte("some auth key"),
		Hash:     []byte("oooooh that's definitely a key hash!"),
		Salt:     0,
		Hostname: "1337.228.1488.0",
	}, sess)
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
