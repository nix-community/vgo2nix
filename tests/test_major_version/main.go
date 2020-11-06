package main

import (
	"fmt"

	replaced "github.com/cespare/xxhash/v2"
	v1 "github.com/yihuang/test-golang-module-major-version"
	v1_sub "github.com/yihuang/test-golang-module-major-version/sub"
	v2_sub "github.com/yihuang/test-golang-module-major-version/sub/v2"
	v2 "github.com/yihuang/test-golang-module-major-version/v2"
	"gopkg.in/yaml.v2"
)

func main() {
	fmt.Println(v1.Test(), v2.Test(), v1_sub.Test(), v2_sub.Test(), replaced.Test())
	fmt.Println(yaml.Marshal(1))
}
