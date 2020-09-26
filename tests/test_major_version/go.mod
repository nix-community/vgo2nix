module testmod

go 1.15

require github.com/yihuang/test-golang-module-major-version v1.0.1
require github.com/yihuang/test-golang-module-major-version/v2 v2.0.1
require github.com/yihuang/test-golang-module-major-version/sub v1.0.1
require github.com/yihuang/test-golang-module-major-version/sub/v2 v2.0.1
require github.com/cespare/xxhash/v2 v2.1.1
replace github.com/cespare/xxhash/v2 v2.1.1 => github.com/yihuang/test-golang-module-major-version/v2 v2.0.2-0.20200924181748-4a13e4bfd5a9
