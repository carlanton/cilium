// Copyright 2016-2018 Authors of Cilium
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v2

// Rules is a collection of api.Rule.
//
// All rules must be evaluated in order to come to a conclusion. While
// it is sufficient to have a single fromEndpoints rule match, none of
// the fromRequires may be violated at the same time.
type Rules []*Rule

func (rs *Rules) Sanitize() error {
	if rs == nil {
		return nil
	}
	for _, r := range *rs {
		err := r.Sanitize()
		if err != nil {
			return err
		}
	}
	return nil
}