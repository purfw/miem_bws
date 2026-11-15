package support

import "github.com/confhub/shared/openapi"

func APIModels() map[string]openapi.Models {
	return map[string]openapi.Models{"support.(*Handler).Create": {Request: createReq{}, Response: createResp{}, Status: 202}}
}
