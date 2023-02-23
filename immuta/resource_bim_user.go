// Package immuta resource bim user creates an Immuta user via the bim iam provider API
package immuta

import (
	"context"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/instacart/terraform-provider-immuta/client"
	"github.com/pkg/errors"
)

type BimUserInput struct {
	Userid      string              `json:"userid"`
	Password    string              `json:"password"`
	Profile     BimUserProfileInput `json:"profile"`
	Permissions []interface{}       `json:"permissions,omitempty"`
}

type BimUser struct {
	Userid  string         `json:"userid"`
	Iamid   string         `json:"iamid"`
	Profile BimUserProfile `json:"profile"`
}

type BimUserProfileInput struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

type BimUserProfile struct {
	BimUserProfileInput
	ExternalUserIds struct {
		SnowflakeUser string `json:"snowflakeUser,omitempty"`
	} `json:"externalUserIds,omitempty"`
}

type BimUserCreateResponse struct {
	NewUser struct {
		BimUser
	} `json:"newUser"`
}

type BimUsers struct {
	Users []BimUser `json:"users"`
	Count int       `json:"count"`
}

type BimUserAPI struct {
	client *client.ImmutaClient
}

func NewBimUserAPI(client *client.ImmutaClient) *BimUserAPI {
	return &BimUserAPI{
		client: client,
	}
}

func (a *BimUserAPI) ListBimUsers() (bimUserResponse *BimUser, err error) {
	err = a.client.Get("/bim/iam/bim/user", "", map[string]string{}, &bimUserResponse)
	return
}

func (a *BimUserAPI) GetBimUser(userid string) (bimUserResponse *BimUser, err error) {
	err = a.client.Get("/bim/iam/bim/user/"+userid, "", map[string]string{}, &bimUserResponse)
	return
}

func (a *BimUserAPI) CreateBimUser(bimUser *BimUserInput) (bimUserResponse *BimUserCreateResponse, err error) {
	err = a.client.Post("/bim/iam/bim/user", "", bimUser, &bimUserResponse)
	return
}

func (a *BimUserAPI) DeleteBimUser(userid string) (err error) {
	err = a.client.Delete("/bim/iam/bim/user/"+userid, "", nil, nil)
	return
}

//func (a *BimUserAPI) UpdateBimUser(userid string, profile *BimUserProfile) (bimUserResponse *BimUser, err error) {
//	err = a.client.Put("/bim/iam/bim/user/"+userid+"/profile", "", profile, &profile)
//	return
//}

func (a *BimUserAPI) UpdateBimUserProfile(userid string, profile *BimUserProfile) (bimUserResponse *BimUser, err error) {
	err = a.client.Put("/bim/iam/bim/user/"+userid+"/profile", "", profile, &profile)
	return
}

func ResourceBimUser() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceBimUserCreate,
		ReadContext:   resourceBimUserRead,
		UpdateContext: resourceBimUserUpdate,
		DeleteContext: resourceBimUserDelete,

		Schema: map[string]*schema.Schema{
			"userid": {
				Type:     schema.TypeString,
				Required: true,
			},
			"password": {
				Type:     schema.TypeString,
				Required: true,
			},
			"name": {
				Type:     schema.TypeString,
				Optional: true,
				// defaults to userid
				Computed: true,
			},
			"email": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"snowflake_user": {
				Type:     schema.TypeString,
				Optional: true,
			},
		},
	}
}

func resourceBimUserCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	client := m.(*client.ImmutaClient)
	bimUserAPI := NewBimUserAPI(client)

	bimUserInput := BimUserInput{}
	bimUserInput.Userid = d.Get("userid").(string)
	bimUserInput.Password = d.Get("password").(string)

	name, nameExists := d.GetOk("name")
	if nameExists == true {
		bimUserInput.Profile.Name = name.(string)
	} else {
		bimUserInput.Profile.Name = bimUserInput.Userid
	}

	bimUserInput.Profile.Email = d.Get("email").(string)

	bimUser, err := bimUserAPI.CreateBimUser(&bimUserInput)
	if err != nil {
		return diag.FromErr(errors.Wrap(err, "Error creating BIM user"))
	}

	if bimUser.NewUser.Userid == "" {
		return diag.FromErr(errors.Wrap(err, "No user id in response"))
	}

	d.SetId(bimUser.NewUser.Userid)

	// if name not set in resource but implied from userid, update the resource name
	if nameExists == false {
		d.Set("name", bimUser.NewUser.Profile.Name)
	}

	// Need to update profile after creation as cannot assign Snowflake user ID during creation
	if snowflakeUser, exists := d.GetOk("snowflake_user"); exists == true {

		bimUserProfile := BimUserProfile{}
		bimUserProfile.ExternalUserIds.SnowflakeUser = snowflakeUser.(string)

		_, err := bimUserAPI.UpdateBimUserProfile(bimUser.NewUser.Userid, &bimUserProfile)
		if err != nil {
			return diag.FromErr(errors.Wrap(err, "Error updating BIM user profile with snowflake ID"))
		}
	}

	return diags
}

func resourceBimUserRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	client := m.(*client.ImmutaClient)
	bimUserAPI := NewBimUserAPI(client)

	bimUser, err := bimUserAPI.GetBimUser(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	d.Set("userid", bimUser.Userid)
	d.Set("name", bimUser.Profile.Name)
	d.Set("email", bimUser.Profile.Email)
	d.Set("snowflake_user", bimUser.Profile.ExternalUserIds.SnowflakeUser)

	return diags
}

func resourceBimUserDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	client := m.(*client.ImmutaClient)
	bimUserAPI := NewBimUserAPI(client)

	err := bimUserAPI.DeleteBimUser(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	return diags
}

func resourceBimUserUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	client := m.(*client.ImmutaClient)
	bimUserAPI := NewBimUserAPI(client)

	bimUserProfile := BimUserProfile{}

	bimUserProfile.Email = d.Get("email").(string)
	bimUserProfile.ExternalUserIds.SnowflakeUser = d.Get("snowflake_user").(string)

	if name, exists := d.GetOk("name"); exists == true {
		bimUserProfile.Name = name.(string)
	} else {
		bimUserProfile.Name = d.Get("userid").(string)
	}

	bimUser, err := bimUserAPI.UpdateBimUserProfile(d.Id(), &bimUserProfile)
	if err != nil {
		return diag.FromErr(errors.Wrap(err, "Error updating BIM user profile."))
	}

	d.SetId(bimUser.Userid)

	return diags
}
