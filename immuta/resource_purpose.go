package immuta

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/instacart/terraform-provider-immuta/client"
	"time"
)

type PurposeInput struct {
	Name            string `json:"name"`
	Acknowledgement string `json:"acknowledgement"`
	Description     string `json:"description"`
}

type Purpose struct {
	PurposeInput
	Id                     int         `json:"id"`
	AddedByProfile         int         `json:"addedByProfile"`
	DisplayAcknowledgement bool        `json:"displayAcknowledgement"`
	Deleted                bool        `json:"deleted"`
	SystemGenerated        bool        `json:"systemGenerated"`
	PolicyMetadata         interface{} `json:"policyMetadata"`
	CreatedAt              time.Time   `json:"createdAt"`
	UpdatedAt              time.Time   `json:"updatedAt"`
}

type PurposeResourceResponse struct {
	DryRun    bool `json:"dryRun"`
	Creating  bool `json:"creating"`
	Updating  bool `json:"updating"`
	PurposeId int  `json:"purposeId"`
}

type Purposes struct {
	Purposes []Purpose `json:"purposes"`
	Count    int       `json:"count"`
}

type PurposeAPI struct {
	client *client.ImmutaClient
}

func NewPurposeAPI(m any) PurposeAPI {
	return PurposeAPI{
		client: m.(*client.ImmutaClient),
	}
}

func (a *PurposeAPI) ListPurposes() (purposes Purposes, err error) {
	err = a.client.Get("/governance/purpose", "", map[string]string{"noLimit": "false"}, &purposes)
	return
}

func (a *PurposeAPI) GetPurpose(id string) (purpose Purpose, err error) {
	err = a.client.Get(fmt.Sprintf("/governance/purpose/%s", id), "", nil, &purpose)
	return
}

func (a *PurposeAPI) DeletePurpose(id string) (err error) {
	err = a.client.Delete(fmt.Sprintf("/governance/purpose/%s", id), "", nil, nil)
	return
}

func (a *PurposeAPI) UpsertPurpose(purpose PurposeInput) (purposeResponse PurposeResourceResponse, err error) {
	err = a.client.Post("/api/v2/purpose", "", purpose, &purposeResponse)
	return
}

func ResourcePurpose() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourcePurposeCreate,
		ReadContext:   resourcePurposeRead,
		UpdateContext: resourcePurposeUpdate,
		DeleteContext: resourcePurposeDelete,

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
			},
			"description": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"acknowledgement": {
				Type:     schema.TypeString,
				Optional: true,
			},
		},
	}
}

func resourcePurposeCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	api := NewPurposeAPI(m)

	var ack string
	if acknowledgement, exists := d.GetOk("acknowledgement"); exists {
		ack = acknowledgement.(string)
	}
	var purposeResponse PurposeResourceResponse

	// Do it twice as a workaround for a bug in the API where acknowledgement not updated first time
	for i := 0; i < 2; i++ {
		pr, err := api.UpsertPurpose(PurposeInput{
			Name:            d.Get("name").(string),
			Description:     d.Get("description").(string),
			Acknowledgement: ack,
		})
		if err != nil {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  fmt.Sprintf("Could not create purpose: %s", err),
			})
			return diags
		} else {
			purposeResponse = pr
		}
	}

	d.SetId(fmt.Sprintf("%d", purposeResponse.PurposeId))

	return diags
}

func resourcePurposeRead(c context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	api := NewPurposeAPI(m)
	purpose, err := api.GetPurpose(d.Id())
	if err != nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Could not get purpose with ID: " + d.Id(),
		})
		return diags
	}

	d.Set("name", purpose.Name)
	d.Set("description", purpose.Description)
	d.Set("acknowledgement", purpose.Acknowledgement)

	return diags
}

func resourcePurposeUpdate(c context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	// Use the Immuta client to update the purpose using the data in the resource data
	api := NewPurposeAPI(m)
	purposeResponse, err := api.UpsertPurpose(PurposeInput{
		Name:            d.Get("name").(string),
		Description:     d.Get("description").(string),
		Acknowledgement: d.Get("acknowledgement").(string),
	})
	if err != nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Could not update purpose",
		})
		return diags
	}

	if fmt.Sprintf("%d", purposeResponse.PurposeId) != d.Id() {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Purpose ID changed after upsert operation",
		})
		return diags
	}

	return diags
}

func resourcePurposeDelete(c context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	// Use the Immuta client to delete the purpose using the data in the resource data
	api := NewPurposeAPI(m)
	err := api.DeletePurpose(d.Id())
	if err != nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Could not delete purpose",
		})
		return diags
	}

	return diags
}
