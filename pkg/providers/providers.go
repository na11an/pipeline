// Copyright © 2018 Banzai Cloud
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

package providers

import (
	"database/sql/driver"

	pkgErrors "github.com/banzaicloud/pipeline/pkg/errors"
	"github.com/banzaicloud/pipeline/pkg/providers/alibaba"
	"github.com/banzaicloud/pipeline/pkg/providers/amazon"
	"github.com/banzaicloud/pipeline/pkg/providers/azure"
	"github.com/banzaicloud/pipeline/pkg/providers/google"
	"github.com/banzaicloud/pipeline/pkg/providers/oracle"
)

// ProviderID represents the identifier of a provider
type ProviderID string

// Value returns the value of the ID
func (id ProviderID) Value() (driver.Value, error) {
	// TODO: remove Valuer implementation when mysql driver version is >=1.4
	return string(id), nil
}

const (
	// Alibaba is the ID of the Alibaba Cloud cloud provider
	Alibaba ProviderID = alibaba.Provider
	// Amazon is the ID of the Amazon Web Services cloud provider
	Amazon ProviderID = amazon.Provider
	// Azure is the ID of the Microsoft Azure cloud provider
	Azure ProviderID = azure.Provider
	// Google is the ID of the Google Cloud Platform cloud provider
	Google ProviderID = google.Provider
	// Oracle is the ID of the Oracle Cloud cloud provider
	Oracle ProviderID = oracle.Provider

	// Unknown represents an unknown provider ID
	Unknown ProviderID = "unknown"

	BucketCreating    = "CREATING"
	BucketCreated     = "AVAILABLE"
	BucketCreateError = "ERROR_CREATE"

	BucketDeleting    = "DELETING"
	BucketDeleted     = "DELETED"
	BucketDeleteError = "ERROR_DELETE"
)

// ValidateProvider validates if the passed cloud provider is supported.
// Unsupported cloud providers trigger an pkgErrors.ErrorNotSupportedCloudType error.
func ValidateProvider(provider ProviderID) error {
	switch provider {
	case Alibaba:
	case Amazon:
	case Google:
	case Azure:
	case Oracle:
	default:
		// TODO: create an error value in this package instead
		return pkgErrors.ErrorNotSupportedCloudType
	}

	return nil
}
