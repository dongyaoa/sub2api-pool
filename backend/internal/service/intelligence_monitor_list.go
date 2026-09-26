package service

import "context"

func (s *IntelligenceMonitorService) loadPlanListData(ctx context.Context, plans []*IntelligenceMonitorPlan) (*IntelligenceMonitorPlanListData, error) {
	data := &IntelligenceMonitorPlanListData{Runs: map[int64][]*IntelligenceMonitorRun{}, SourceNames: map[int64]string{}}
	if len(plans) == 0 {
		return data, nil
	}
	if repo, ok := s.repo.(IntelligenceMonitorListRepository); ok {
		ids := make([]int64, 0, len(plans))
		for _, p := range plans {
			ids = append(ids, p.ID)
		}
		return repo.LoadPlanListData(ctx, ids)
	}
	// Compatibility for repository decorators and in-memory test stores. The SQL
	// repository implements the batched path, so normal polling never uses this.
	for _, p := range plans {
		page, err := s.repo.ListRuns(ctx, IntelligenceMonitorRunQuery{PlanID: &p.ID, Page: 1, PageSize: IntelligenceMonitorRetainedRuns + 1})
		if err != nil {
			return nil, err
		}
		data.Runs[p.ID] = page.Items
		switch p.SourceType {
		case "openai_oauth":
			if p.AccountID != nil && s.accounts != nil {
				if account, err := s.accounts.GetByID(ctx, *p.AccountID); err == nil && account != nil {
					data.SourceNames[p.ID] = account.Name
				}
			}
		case "upstream":
			if p.UpstreamTargetID != nil && s.upstreams != nil {
				if target, err := s.upstreams.GetTarget(ctx, *p.UpstreamTargetID); err == nil && target != nil {
					data.SourceNames[p.ID] = target.Name
				}
			}
		case "local_group":
			if p.GroupID != nil && s.groups != nil {
				if group, err := s.groups.GetByID(ctx, *p.GroupID); err == nil && group != nil {
					data.SourceNames[p.ID] = group.Name
				}
			}
		}
	}
	return data, nil
}
