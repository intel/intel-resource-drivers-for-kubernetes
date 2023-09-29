/*
 * Copyright (c) 2023, Intel Corporation.  All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import "fmt"

type stringSearchMap map[string]bool

func mapKeys[K comparable, V any](aMap map[K]V) []K {
	keys := make([]K, len(aMap))

	i := 0
	for key := range aMap {
		keys[i] = key
		i++
	}

	return keys
}

func (sm stringSearchMap) String() string {
	return fmt.Sprintf("%v", mapKeys(sm))
}

func newSearchMap[K comparable](values ...K) map[K]bool {
	sm := map[K]bool{}
	for _, value := range values {
		sm[value] = true
	}

	return sm
}
