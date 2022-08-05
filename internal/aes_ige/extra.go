// Copyright (c) 2022 RoseLoverX

package ige

type any = interface{}
type null = struct{}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
