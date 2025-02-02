/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package builder

import (
	"net/http"
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"
)

// WebhookBuilder builds a Webhook.
type WebhookBuilder struct {
	apiType runtime.Object
	gvk     schema.GroupVersionKind
	mgr     manager.Manager
	config  *rest.Config
}

func WebhookManagedBy(m manager.Manager) *WebhookBuilder {
	return &WebhookBuilder{mgr: m}
}

// TODO(droot): update the GoDoc for conversion.

// For takes a runtime.Object which should be a CR.
// If the given object implements the admission.Defaulter interface, a MutatingWebhook will be wired for this type.
// If the given object implements the admission.Validator interface, a ValidatingWebhook will be wired for this type.
func (blder *WebhookBuilder) For(apiType runtime.Object) *WebhookBuilder {
	blder.apiType = apiType
	return blder
}

// Complete builds the webhook.
func (blder *WebhookBuilder) Complete() error {
	// Set the Config
	if err := blder.loadRestConfig(); err != nil {
		return err
	}

	// Set the Webook if needed
	return blder.registerWebhooks()
}

func (blder *WebhookBuilder) loadRestConfig() error {
	if blder.config != nil {
		return nil
	}
	if blder.mgr != nil {
		blder.config = blder.mgr.GetConfig()
		return nil
	}
	var err error
	blder.config, err = getConfig()
	return err
}

func (blder *WebhookBuilder) registerWebhooks() error {
	// Create webhook(s) for each type
	var err error
	blder.gvk, err = apiutil.GVKForObject(blder.apiType, blder.mgr.GetScheme())
	if err != nil {
		return err
	}

	blder.registerDefaultingWebhook()
	blder.registerValidatingWebhook()

	err = conversion.CheckConvertibility(blder.mgr.GetScheme(), blder.apiType)
	if err != nil {
		log.Error(err, "conversion check failed", "GVK", blder.gvk)
	}
	return nil
}

// registerDefaultingWebhook registers a defaulting webhook if th
func (blder *WebhookBuilder) registerDefaultingWebhook() {
	if defaulter, isDefaulter := blder.apiType.(admission.Defaulter); isDefaulter {
		mwh := admission.DefaultingWebhookFor(defaulter)
		if mwh != nil {
			path := generateMutatePath(blder.gvk)

			// Checking if the path is already registered.
			// If so, just skip it.
			if !blder.isAlreadyHandled(path) {
				log.Info("Registering a mutating webhook",
					"GVK", blder.gvk,
					"path", path)
				blder.mgr.GetWebhookServer().Register(path, mwh)
			}
		}
	}
}

func (blder *WebhookBuilder) registerValidatingWebhook() {
	if validator, isValidator := blder.apiType.(admission.Validator); isValidator {
		vwh := admission.ValidatingWebhookFor(validator)
		if vwh != nil {
			path := generateValidatePath(blder.gvk)

			// Checking if the path is already registered.
			// If so, just skip it.
			if !blder.isAlreadyHandled(path) {
				log.Info("Registering a validating webhook",
					"GVK", blder.gvk,
					"path", path)
				blder.mgr.GetWebhookServer().Register(path, vwh)
			}
		}
	}
}

func (blder *WebhookBuilder) isAlreadyHandled(path string) bool {
	h, p := blder.mgr.GetWebhookServer().WebhookMux.Handler(&http.Request{URL: &url.URL{Path: path}})
	if p == path && h != nil {
		return true
	}
	return false
}

func generateMutatePath(gvk schema.GroupVersionKind) string {
	return "/mutate-" + strings.Replace(gvk.Group, ".", "-", -1) + "-" +
		gvk.Version + "-" + strings.ToLower(gvk.Kind)
}

func generateValidatePath(gvk schema.GroupVersionKind) string {
	return "/validate-" + strings.Replace(gvk.Group, ".", "-", -1) + "-" +
		gvk.Version + "-" + strings.ToLower(gvk.Kind)
}
