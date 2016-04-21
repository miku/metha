package metha

type Repository struct {
	BaseURL string
}

func (r Repository) Formats() ([]MetadataFormat, error) {
	var formats []MetadataFormat
	var token string
	for {
		req := Request{BaseURL: r.BaseURL, Verb: "ListMetadataFormats", ResumptionToken: token}
		resp, err := Do(&req)
		if err != nil {
			return nil, err
		}
		formats = append(formats, resp.ListMetadataFormats.MetadataFormat...)
		if !resp.HasResumptionToken() {
			break
		}
		token = resp.GetResumptionToken()
	}
	return formats, nil
}

func (r Repository) Sets() ([]Set, error) {
	var sets []Set
	var token string
	for {
		req := Request{BaseURL: r.BaseURL, Verb: "ListSets", ResumptionToken: token}
		resp, err := Do(&req)
		if err != nil {
			return nil, err
		}
		sets = append(sets, resp.ListSets.Set...)
		if !resp.HasResumptionToken() {
			break
		}
		token = resp.GetResumptionToken()
	}
	return sets, nil
}
