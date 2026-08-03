package export

import (
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// imageRelatedGroupKinds lists the OCP resource kinds that reference or produce
// image content. Crane exports these like any other resource, but does not
// migrate the underlying image data itself — that remains the user's
// responsibility (see https://github.com/migtools/crane/issues/681).
var imageRelatedGroupKinds = map[schema.GroupKind]string{
	{Group: "build.openshift.io", Kind: "BuildConfig"}: "defines an image build; crane does not build or push images — " +
		"build/push it separately (e.g. `oc start-build`) and ensure the resulting image is available on the target cluster",
	{Group: "image.openshift.io", Kind: "ImageStream"}: "tracks image tags that crane does not migrate — " +
		"use `crane skopeo-sync-gen` to generate a sync manifest, then run `skopeo sync` separately to copy the underlying images to the target registry",
	{Group: "image.openshift.io", Kind: "ImageStreamTag"}: "references image content that crane does not migrate — " +
		"see `crane skopeo-sync-gen` to sync the underlying image separately",
}

// imageResourceGuidance returns the user-facing guidance for a given
// apiGroup/kind pair if it's a known image-related resource, and whether a
// match was found.
func imageResourceGuidance(apiGroup, kind string) (string, bool) {
	msg, ok := imageRelatedGroupKinds[schema.GroupKind{Group: apiGroup, Kind: kind}]
	return msg, ok
}

// warnAboutImageResources logs a warning for every exported resource that
// crane recognizes as image-related, naming the resource and telling the
// user what to do next.
func warnAboutImageResources(resources []*groupResource, log *logrus.Logger) {
	for _, g := range resources {
		if g == nil || g.objects == nil {
			continue
		}
		msg, ok := imageResourceGuidance(g.APIGroup, g.APIResource.Kind)
		if !ok {
			continue
		}
		for _, obj := range g.objects.Items {
			name := obj.GetName()
			if ns := obj.GetNamespace(); ns != "" {
				name = ns + "/" + name
			}
			log.Warnf("exported %s %q — %s", g.APIResource.Kind, name, msg)
		}
	}
}
