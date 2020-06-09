package applier

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//Client structure to access kubernetes through the applier
type Client struct {
	//An Applier
	applier *Applier
	//The client-go kubernetes client
	client client.Client
	//The owner of the created object
	owner metav1.Object
	//The scheme
	scheme *runtime.Scheme
	//A merger defining how two objects must be merged
	merger Merger
}

//NewApplierClient creates a new client to access kubernetes through the applier.
//applier: An applier
//client: The client-go client to use when applying the resources.
//owner: The object owner for the setControllerReference, the reference is not if nil.
//scheme: The object scheme for the setControllerReference, the reference is not if nil.
//merger: The function implementing the way how the resources must be merged
func NewApplierClient(
	applier *Applier,
	client client.Client,
	owner metav1.Object,
	scheme *runtime.Scheme,
	merger Merger,
) (*Client, error) {
	return &Client{
		applier: applier,
		client:  client,
		owner:   owner,
		scheme:  scheme,
		merger:  merger,
	}, nil
}

//Merger merges the `current` and the `want` resources into one resource which will be use for to update.
// If `update` is true than the update will be executed.
type Merger func(current,
	new *unstructured.Unstructured,
) (
	future *unstructured.Unstructured,
	update bool,
)

//CreateOrUpdateInPath creates or updates the assets (if they don't exist yet) found in the path and
// subpath if recursive is set to true.
// path: The path were the yaml to apply is located
// excludes: The list of yamls to exclude
// recursive: If true all yamls in the path directory and sub-directories will be applied
// it excludes the assets named in the excluded array
// it sets the Controller reference if owner and scheme are not nil
//
func (c *Client) CreateOrUpdateInPath(
	path string,
	excluded []string,
	recursive bool,
) error {
	us, err := c.applier.TemplateAssetsInPathUnstructured(
		path,
		excluded,
		recursive)

	if err != nil {
		return err
	}
	//Create the unstructured items if they don't exist yet
	for _, u := range us {
		err := c.createOrUpdate(u)
		if err != nil {
			return err
		}
	}
	return nil
}

//createOrUpdate creates or updates an unstructured (if they don't exist yet) found in the path and
func (c *Client) createOrUpdate(
	u *unstructured.Unstructured,
) error {

	log.Info("Create or update", "Kind", u.GetKind(), "Name", u.GetName(), "Namespace", u.GetNamespace())
	//Set controller ref
	if c.owner != nil && c.scheme != nil {
		if err := controllerutil.SetControllerReference(c.owner, u, c.scheme); err != nil {
			log.Error(err, "Failed to SetControllerReference",
				"Name", u.GetName(),
				"Namespace", u.GetNamespace())
			return err
		}
	}

	//Check if already exists
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(u.GroupVersionKind())
	var errGet error
	errGet = c.client.Get(context.TODO(), types.NamespacedName{Name: u.GetName(), Namespace: u.GetNamespace()}, current)
	if errGet != nil {
		if errors.IsNotFound(errGet) {
			log.Info("Create",
				"Kind", current.GetKind(),
				"Name", current.GetName(),
				"Namespace", current.GetNamespace())
			err := c.client.Create(context.TODO(), u)
			if err != nil {
				log.Error(err, "Unable to create", "Kind", u.GetKind(), "Name", u.GetName(), "Namespace", u.GetNamespace())
				return err
			}
		} else {
			log.Error(errGet, "Error while create", "Kind", u.GetKind(), "Name", u.GetName(), "Namespace", u.GetNamespace())
			return errGet
		}
	} else {
		log.Info("Update",
			"Kind", current.GetKind(),
			"Name", current.GetName(),
			"Namespace", current.GetNamespace())

		future, update := c.merger(current, u)
		if update {
			err := c.client.Update(context.TODO(), future)
			if err != nil {
				log.Error(err, "Error while update", "Kind", u.GetKind(), "Name", u.GetName(), "Namespace", u.GetNamespace())
				return err
			}
		} else {
			log.Info("No update needed")
		}
	}
	return nil
}