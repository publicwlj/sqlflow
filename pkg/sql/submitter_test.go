// Copyright 2019 The SQLFlow Authors. All rights reserved.
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

package sql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubmitterRegistry(t *testing.T) {
	a := assert.New(t)
	a.Equal(4, len(submitterRegistry))
	a.NotNil(submitterRegistry["pai"])
	a.NotNil(submitterRegistry["default"])
	a.NotNil(submitterRegistry["elasticdl"])
	a.NotNil(submitterRegistry["alps"])
	a.Equal(submitter(), submitterRegistry["default"])
}
