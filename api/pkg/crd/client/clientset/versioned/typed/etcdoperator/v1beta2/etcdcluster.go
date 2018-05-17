package v1beta2

import (
	scheme "github.com/kubermatic/kubermatic/api/pkg/crd/client/clientset/versioned/scheme"
	v1beta2 "github.com/kubermatic/kubermatic/api/pkg/crd/etcdoperator/v1beta2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// EtcdClustersGetter has a method to return a EtcdClusterInterface.
// A group's client should implement this interface.
type EtcdClustersGetter interface {
	EtcdClusters(namespace string) EtcdClusterInterface
}

// EtcdClusterInterface has methods to work with EtcdCluster resources.
type EtcdClusterInterface interface {
	Create(*v1beta2.EtcdCluster) (*v1beta2.EtcdCluster, error)
	Update(*v1beta2.EtcdCluster) (*v1beta2.EtcdCluster, error)
	UpdateStatus(*v1beta2.EtcdCluster) (*v1beta2.EtcdCluster, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1beta2.EtcdCluster, error)
	List(opts v1.ListOptions) (*v1beta2.EtcdClusterList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1beta2.EtcdCluster, err error)
	EtcdClusterExpansion
}

// etcdClusters implements EtcdClusterInterface
type etcdClusters struct {
	client rest.Interface
	ns     string
}

// newEtcdClusters returns a EtcdClusters
func newEtcdClusters(c *EtcdV1beta2Client, namespace string) *etcdClusters {
	return &etcdClusters{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the etcdCluster, and returns the corresponding etcdCluster object, and an error if there is any.
func (c *etcdClusters) Get(name string, options v1.GetOptions) (result *v1beta2.EtcdCluster, err error) {
	result = &v1beta2.EtcdCluster{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("etcdclusters").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of EtcdClusters that match those selectors.
func (c *etcdClusters) List(opts v1.ListOptions) (result *v1beta2.EtcdClusterList, err error) {
	result = &v1beta2.EtcdClusterList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("etcdclusters").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested etcdClusters.
func (c *etcdClusters) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("etcdclusters").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a etcdCluster and creates it.  Returns the server's representation of the etcdCluster, and an error, if there is any.
func (c *etcdClusters) Create(etcdCluster *v1beta2.EtcdCluster) (result *v1beta2.EtcdCluster, err error) {
	result = &v1beta2.EtcdCluster{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("etcdclusters").
		Body(etcdCluster).
		Do().
		Into(result)
	return
}

// Update takes the representation of a etcdCluster and updates it. Returns the server's representation of the etcdCluster, and an error, if there is any.
func (c *etcdClusters) Update(etcdCluster *v1beta2.EtcdCluster) (result *v1beta2.EtcdCluster, err error) {
	result = &v1beta2.EtcdCluster{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("etcdclusters").
		Name(etcdCluster.Name).
		Body(etcdCluster).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *etcdClusters) UpdateStatus(etcdCluster *v1beta2.EtcdCluster) (result *v1beta2.EtcdCluster, err error) {
	result = &v1beta2.EtcdCluster{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("etcdclusters").
		Name(etcdCluster.Name).
		SubResource("status").
		Body(etcdCluster).
		Do().
		Into(result)
	return
}

// Delete takes name of the etcdCluster and deletes it. Returns an error if one occurs.
func (c *etcdClusters) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("etcdclusters").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *etcdClusters) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("etcdclusters").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched etcdCluster.
func (c *etcdClusters) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1beta2.EtcdCluster, err error) {
	result = &v1beta2.EtcdCluster{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("etcdclusters").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
