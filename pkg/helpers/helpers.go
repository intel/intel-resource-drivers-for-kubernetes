/*
 * Copyright (c) 2024, Intel Corporation.  All Rights Reserved.
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

package helpers

import (
	"fmt"
	"os"
)

func WriteFile(filePath string, fileContents string) error {
	fhandle, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("could not create file %v: %v", filePath, err)
	}

	if _, err = fhandle.WriteString(fileContents); err != nil {
		return fmt.Errorf("could not write to file %v: %v", filePath, err)
	}

	if err := fhandle.Close(); err != nil {
		return fmt.Errorf("could not close file %v: %v", filePath, err)
	}

	return nil
}
