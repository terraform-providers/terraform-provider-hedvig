package hedvig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
)

type createMountResponse struct {
	Result struct {
		ExportInfo []struct {
			Target  string `json:"target"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"exportInfo"`
	} `json:"result"`
	RequestID string `json:"requestId"`
	Type      string `json:"type"`
	Message   string `json:"message"`
	Status    string `json:"status"`
}

type createNFSMountCheck struct {
	Result []struct {
		Protocol string `json:"protocol"`
		Target   string `json:"target"`
	}
	Status    string `json:"status"`
	Type      string `json:"type"`
	RequestId string `json:"requestId`
}

type readMountResponse struct {
	Result    []string `json:"result"`
	RequestID string   `json:"requestId"`
	Type      string   `json:"type"`
	Message   string   `json:"message"`
	Status    string   `json:"status"`
}

type deleteMountResponse struct {
	Result []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"result"`
	RequestID string `json:"requestId"`
	Message   string `json:"message"`
	Type      string `json:"type"`
	Status    string `json:"status"`
}

func resourceMount() *schema.Resource {
	return &schema.Resource{
		Create: resourceMountCreate,
		Read:   resourceMountRead,
		Delete: resourceMountDelete,

		Schema: map[string]*schema.Schema{
			"vdisk": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"controller": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
		},
	}
}

func resourceMountCreate(d *schema.ResourceData, meta interface{}) error {
	u := url.URL{}
	u.Host = meta.(*HedvigClient).Node
	u.Path = "/rest/"
	u.Scheme = "http"

	sessionID, err := GetSessionId(d, meta.(*HedvigClient))
	if err != nil {
		return err
	}

	q := url.Values{}
	q.Set("request", fmt.Sprintf("{type:Mount, category:VirtualDiskManagement, params:{virtualDisk:'%s', targets:['%s']}, sessionId:'%s'}", d.Get("vdisk"), d.Get("controller"), sessionID))

	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		return err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	createResp := createMountResponse{}
	err = json.Unmarshal(body, &createResp)
	if err != nil {
		return err
	}

	if createResp.Status != "ok" {
		return errors.New("Unknown error creating export")
	}

	if len(createResp.Result.ExportInfo) != 1 {
		return errors.New("Error - unexpected response from server")
	}

	if createResp.Result.ExportInfo[0].Status != "ok" {
		if strings.Contains(createResp.Result.ExportInfo[0].Message, "trying to get handle to") {
			q2 := url.Values{}
			q2.Set("request", fmt.Sprintf("{type:ListTargets, category:VirtualDiskManagement, sessionId:'%s'}", sessionID))

			u2 := url.URL{}
			u2.Host = meta.(*HedvigClient).Node
			u2.Path = "/rest/"
			u2.Scheme = "http"

			u2.RawQuery = q2.Encode()

			resp2, err2 := http.Get(u2.String())
			if err2 != nil {
				return err2
			}

			body2, err := ioutil.ReadAll(resp2.Body)
			if err != nil {
				return err
			}

			createResp2 := createNFSMountCheck{}
			err = json.Unmarshal(body2, &createResp2)
			if err != nil {
				return err
			}

			if len(createResp2.Result) < 1 {
				return errors.New("No controllers found")
			} else {
				for i := 0; i < len(createResp2.Result); i++ {
					if createResp2.Result[i].Protocol != "nfs" {
						continue
					} else {
						return fmt.Errorf("Given controller not NFS -- try %s", createResp2.Result[i].Target)
					}
				}
				return fmt.Errorf("No NFS controllers available")
			}
		}

		return fmt.Errorf("Error creating export: %s", createResp.Result.ExportInfo[0].Message)
	}

	d.SetId("mount$" + d.Get("vdisk").(string) + "$" + d.Get("controller").(string))

	return resourceMountRead(d, meta)
}

func resourceMountRead(d *schema.ResourceData, meta interface{}) error {
	u := url.URL{}
	u.Host = meta.(*HedvigClient).Node
	u.Path = "/rest/"
	u.Scheme = "http"

	sessionID, err := GetSessionId(d, meta.(*HedvigClient))
	if err != nil {
		return err
	}

	idSplit := strings.Split(d.Id(), "$")
	if len(idSplit) != 3 {
		return fmt.Errorf("Invalid ID: %s", d.Id())
	}

	q := url.Values{}
	q.Set("request", fmt.Sprintf("{type:ListExportedTargets,category:VirtualDiskManagement,params:{virtualDisk:'%s'},sessionId:'%s'}", idSplit[1], sessionID))

	u.RawQuery = q.Encode()
	resp, err := http.Get(u.String())
	if err != nil {
		return err
	}

	if resp.StatusCode == 404 {
		return errors.New("Malformed query; aborting")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	readResp := readMountResponse{}
	err = json.Unmarshal(body, &readResp)
	if err != nil {
		return err
	}

	if readResp.Status == "warning" && strings.HasSuffix(readResp.Message, "t be found") {
		d.SetId("")
		log.Printf("Mount %s not found, clearing from state", idSplit[1])
		return nil
	}

	if readResp.Status != "ok" {
		return fmt.Errorf("Error: %s", readResp.Message)
	}

	if len(readResp.Result) < 1 {
		return errors.New("Error - unexpected response from server")
	}

	for _, rec := range readResp.Result {
		if rec == idSplit[2] {
			d.Set("controller", rec)
			//TODO: Set everything
			return nil
		}
	}
	return errors.New("Resource not found")
}

func resourceMountDelete(d *schema.ResourceData, meta interface{}) error {
	u := url.URL{}
	u.Host = meta.(*HedvigClient).Node
	u.Path = "/rest/"
	u.Scheme = "http"

	sessionID, err := GetSessionId(d, meta.(*HedvigClient))
	if err != nil {
		return err
	}

	idSplit := strings.Split(d.Id(), "$")
	if len(idSplit) != 3 {
		return fmt.Errorf("Invalid ID: %s", d.Id())
	}

	q := url.Values{}
	q.Set("request", fmt.Sprintf("{type:Unmount, category:VirtualDiskManagement, params:{virtualDisk:'%s', targets:['%s']}, sessionId: '%s'}", idSplit[1], idSplit[2], sessionID))

	u.RawQuery = q.Encode()
	resp, err := http.Get(u.String())
	if err != nil {
		return err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	deleteResp := deleteMountResponse{}
	err = json.Unmarshal(body, &deleteResp)
	if err != nil {
		return err
	}

	if deleteResp.Status != "ok" {
		return fmt.Errorf("Error deleting mount: %s", deleteResp.Message)
	}
	return nil
}
