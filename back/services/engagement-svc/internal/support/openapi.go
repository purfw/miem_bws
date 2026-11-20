package support

import "github.com/confhub/shared/openapi"

func APIModels() map[string]openapi.Models {
	page := openapi.PageParameters()
	maximum := 200
	page[1].Default, page[1].Maximum = 50, &maximum
	status := openapi.Query("status", "string", "new, in_progress or closed; empty lists every status")
	status.Enum = []string{"new", "in_progress", "closed"}
	return map[string]openapi.Models{
		"support.(*Handler).Create":         {Request: createReq{}, Response: createResp{}, Status: 202},
		"support.(*Handler).AdminList":      {Response: listResp{Data: []messageDTO{{}}}, Parameters: append(page, status)},
		"support.(*Handler).AdminSetStatus": {Request: statusReq{}, Response: messageDTO{}},
	}
}
