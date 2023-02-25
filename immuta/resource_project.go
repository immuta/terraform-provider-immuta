package immuta

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/instacart/terraform-provider-immuta/client"
	"strconv"
	"time"
)

type ProjectInput struct {
	Name               string                 `json:"name"`
	ProjectKey         string                 `json:"projectKey"`
	Description        string                 `json:"description,omitempty"`
	Documentation      string                 `json:"documentation,omitempty"`
	AllowMaskedJoins   bool                   `json:"allowMaskedJoins,omitempty"`
	SubscriptionPolicy map[string]interface{} `json:"subscriptionPolicy,omitempty"`
	Tags               []string               `json:"tags,omitempty"`
	Purposes           []string               `json:"purposes,omitempty"`
}

type Project struct {
	ProjectInput
	Tags      []interface{} `json:"tags"`
	Purposes  []interface{} `json:"purposes"`
	Id        int           `json:"id"`
	Status    string        `json:"status"`
	Deleted   bool          `json:"deleted"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdateAt  time.Time     `json:"updatedAt"`
}

type ProjectResourceResponseV2 struct {
	DryRun                     bool `json:"dryRun"`
	Creating                   bool `json:"creating"`
	Updating                   bool `json:"updating"`
	NumberOfDataSourcesAdded   int  `json:"numberOfDataSourcesAdded"`
	NumberOfDataSourcesRemoved int  `json:"numberOfDataSourcesRemoved"`
	CreatingWorkspace          bool `json:"creatingWorkspace"`
	DeleteWorkspace            bool `json:"deletingWorkspace"`
	ProjectId                  int  `json:"projectId"`
}

type Projects struct {
	Projects []Project `json:"projects"`
	Count    int       `json:"count"`
}

type ProjectAPI struct {
	client *client.ImmutaClient
}

func NewProjectAPI(m any) ProjectAPI {
	return ProjectAPI{
		client: m.(*client.ImmutaClient),
	}
}

func (a *ProjectAPI) ListProjects() (projects Projects, err error) {
	err = a.client.Get("/project", "", map[string]string{"noLimit": "false"}, &projects)
	return
}

func (a *ProjectAPI) GetProject(id string) (project Project, err error) {
	err = a.client.Get(fmt.Sprintf("/project/%s", id), "", nil, &project)
	return
}

func (a *ProjectAPI) DeleteProject(id string) (err error) {
	err = a.client.Delete(fmt.Sprintf("/project/%s", id), "", nil, nil)
	return
}

func (a *ProjectAPI) UpsertProject(project ProjectInput) (projectResponse ProjectResourceResponseV2, err error) {
	err = a.client.Post("/api/v2/project", "", project, &projectResponse)
	return
}

func ResourceProject() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceProjectCreate,
		ReadContext:   resourceProjectRead,
		UpdateContext: resourceProjectUpdate,
		DeleteContext: resourceProjectDelete,

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
			},
			"project_key": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"description": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"documentation": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"allow_masked_joins": {
				Type:     schema.TypeBool,
				Optional: true,
			},
			"subscription_policy": {
				Type:     schema.TypeMap,
				Optional: true,
			},
			"tags": {
				Type:     schema.TypeList,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
			"purposes": {
				Type:     schema.TypeList,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
		},
	}
}

func resourceProjectCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	api := NewProjectAPI(m)

	var tags []string
	for _, tag := range d.Get("tags").([]interface{}) {
		tags = append(tags, tag.(string))
	}

	var purposes []string
	for _, purpose := range d.Get("purposes").([]interface{}) {
		tags = append(purposes, purpose.(string))
	}

	project := ProjectInput{
		Name:               d.Get("name").(string),
		ProjectKey:         d.Get("project_key").(string),
		Description:        d.Get("description").(string),
		Documentation:      d.Get("documentation").(string),
		AllowMaskedJoins:   d.Get("allow_masked_joins").(bool),
		SubscriptionPolicy: d.Get("subscription_policy").(map[string]interface{}),
		Tags:               tags,
		Purposes:           purposes,
	}

	projectResponse, err := api.UpsertProject(project)
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(strconv.Itoa(projectResponse.ProjectId))

	return diags
}

func resourceProjectRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	api := NewProjectAPI(m)

	id := d.Id()
	project, err := api.GetProject(id)
	if err != nil {
		return diag.FromErr(err)
	}

	d.Set("name", project.Name)
	d.Set("project_key", project.ProjectKey)
	d.Set("description", project.Description)
	d.Set("documentation", project.Documentation)
	d.Set("allow_masked_joins", project.AllowMaskedJoins)
	d.Set("subscription_policy", project.SubscriptionPolicy)
	d.Set("tags", project.Tags)
	d.Set("purposes", project.Purposes)

	return diags
}

func resourceProjectUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	api := NewProjectAPI(m)

	project := ProjectInput{
		Name:               d.Get("name").(string),
		ProjectKey:         d.Get("project_key").(string),
		Description:        d.Get("description").(string),
		Documentation:      d.Get("documentation").(string),
		AllowMaskedJoins:   d.Get("allow_masked_joins").(bool),
		SubscriptionPolicy: d.Get("subscription_policy").(map[string]interface{}),
		Tags:               d.Get("tags").([]string),
		Purposes:           d.Get("purposes").([]string),
	}

	projectResponse, err := api.UpsertProject(project)
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(strconv.Itoa(projectResponse.ProjectId))

	return diags
}

func resourceProjectDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	api := NewProjectAPI(m)

	id := d.Id()
	err := api.DeleteProject(id)
	if err != nil {
		return diag.FromErr(err)
	}

	return diags
}
