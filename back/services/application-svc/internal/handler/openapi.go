package handler

import (
	"github.com/confhub/application-svc/internal/domain"
	"github.com/confhub/shared/openapi"
)

func APIModels() map[string]openapi.Models {
	q := openapi.Query
	page := openapi.PageParameters()
	verification := verificationDTO(&domain.OrganizerVerification{Documents: []domain.VerificationDocument{{}}})
	queueVerification := verificationDTO(&domain.OrganizerVerification{Documents: []domain.VerificationDocument{{}}})
	queueVerification["user_id"], queueVerification["user_email"], queueVerification["user_name"] = "", "", ""
	document := participationDocumentDTO(&domain.ParticipationDocument{})
	url := map[string]any{"url": ""}
	uploadFile := openapi.Upload("file")
	uploadFile.Response = fileDTO{}
	uploadVerification := openapi.Upload("document")
	uploadVerification.Response = verificationDocumentDTO(&domain.VerificationDocument{})
	models := map[string]openapi.Models{
		"(*Applications).submit":                               {Request: submitReq{}, Response: applicationDTO{}, Status: 201, Auth: true},
		"(*Applications).detail":                               {Response: applicationDTO{}, Auth: true},
		"(*Applications).listMine":                             {Response: map[string]any{"items": []applicationDTO{}}, Auth: true},
		"(*Applications).updateMeta":                           {Request: updateMetaReq{}, Response: applicationDTO{}, Auth: true},
		"(*Applications).withdraw":                             {Response: applicationDTO{}, Auth: true},
		"(*Applications).resubmit":                             {Response: applicationDTO{}, Auth: true},
		"(*Applications).createReview":                         {Request: reviewReq{}, Response: reviewDTO{}, Status: 201, Auth: true},
		"(*Applications).listReviews":                          {Response: map[string]any{"items": []reviewDTO{}}, Auth: true},
		"(*Applications).listFiles":                            {Response: map[string]any{"items": []fileDTO{}}, Auth: true},
		"(*Applications).uploadFile":                           uploadFile,
		"(*Applications).listParticipants":                     {Response: participantsResp{}, Parameters: page},
		"(*Applications).createVerification":                   {Request: verificationCreateReq{}, Response: verification, Status: 201, Auth: true},
		"(*Applications).myVerifications":                      {Response: map[string]any{"data": openapi.Array(verification)}, Auth: true},
		"(*Applications).uploadVerificationDocument":           uploadVerification,
		"(*Applications).downloadVerificationDocument":         {Status: 302, OtherResponses: map[int]openapi.Models{200: {Response: url}}, Parameters: []openapi.Parameter{q("redirect", "boolean", "false returns JSON instead of a redirect")}, Auth: true},
		"(*Applications).listParticipationDocuments":           {Response: map[string]any{"data": openapi.Array(document)}, Auth: true},
		"(*Applications).downloadParticipationDocument":        {Response: url, Auth: true},
		"(*Applications).verifyParticipationDocument":          {Response: document},
		"(*Applications).downloadParticipationDocumentByToken": {Status: 302, Parameters: []openapi.Parameter{{Name: "token", In: "query", Required: true, Type: "string", Description: "Expiring document download token"}}},
		"(*Files).download":                                    {Status: 302, OtherResponses: map[int]openapi.Models{200: {Response: url}}, Parameters: []openapi.Parameter{q("redirect", "boolean", "false returns JSON instead of a redirect")}, Auth: true},
		"(*Files).delete":                                      {Status: 204, Auth: true},
		"(*Internal).moderate":                                 {Request: moderateReq{}, Response: applicationDTO{}},
		"(*Internal).list":                                     {Response: map[string]any{"items": []applicationDTO{}, "page": 0, "has_more": false}, Parameters: append(append([]openapi.Parameter{}, page...), q("conference_id", "string", "Conference UUID"), q("user_id", "string", "Applicant UUID"), q("status", "string", "Comma-separated application statuses"))},
		"(*Internal).verificationQueue":                        {Response: map[string]any{"data": openapi.Array(queueVerification), "page": 0, "has_more": false}, Parameters: append(append([]openapi.Parameter{}, page...), q("status", "string", "pending, approved or rejected"))},
		"(*Internal).moderateVerification":                     {Request: verificationModerateReq{}, Response: verification},
		"(*Internal).internalVerificationDocument":             {Response: url},
	}
	for _, name := range []string{"(*Internal).list", "(*Internal).verificationQueue"} {
		model := models[name]
		maximum := 200
		model.Parameters[1].Default, model.Parameters[1].Maximum = 100, &maximum
		models[name] = model
	}
	return models
}
