package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hr-agent/services/internal/agentclient"
	"github.com/hr-agent/services/internal/calendar"
)

// resolveAttendeesWithAgent builds candidate universe + busy, calls Python assign+verify.
func (s *Service) resolveAttendeesWithAgent(ctx context.Context, appID, jdRoundID string, roundIndex int, duration time.Duration) ([]string, map[string]any, error) {
	reqs, err := s.listRoundRequirements(ctx, jdRoundID)
	if err != nil {
		return nil, nil, err
	}
	if len(reqs) == 0 {
		return nil, nil, fmt.Errorf("interviewers_unassigned: no role requirements on round")
	}
	jdDept, _ := s.jdDepartment(ctx, jdRoundID)

	var allCandIDs []string
	seenCand := map[string]bool{}
	for _, req := range reqs {
		role, _ := req["role_kind"].(string)
		poolID, _ := req["pool_id"].(string)
		matchDept := false
		switch v := req["match_jd_department"].(type) {
		case bool:
			matchDept = v
		case int:
			matchDept = v == 1
		}
		needSpecs := parseStringSlice(req["specialties"])
		for _, id := range cleanOpenIDs(parseStringSlice(req["fixed_open_ids"])) {
			if !seenCand[id] {
				seenCand[id] = true
				allCandIDs = append(allCandIDs, id)
			}
		}
		cands, _, err := s.candidatesForRole(ctx, role, poolID, jdDept, needSpecs, matchDept, map[string]bool{})
		if err != nil {
			return nil, nil, err
		}
		for _, id := range cands {
			if !seenCand[id] {
				seenCand[id] = true
				allCandIDs = append(allCandIDs, id)
			}
		}
	}

	profiles, err := s.profilesForOpenIDs(ctx, allCandIDs)
	if err != nil {
		return nil, nil, err
	}
	// ensure fixed-only ids appear as stub candidates
	have := map[string]bool{}
	for _, p := range profiles {
		have[fmt.Sprint(p["open_id"])] = true
	}
	for _, id := range allCandIDs {
		if !have[id] {
			profiles = append(profiles, map[string]any{
				"open_id": id, "name": id, "role_kinds": []any{}, "specialties": []any{}, "enabled": true,
			})
		}
	}

	windowStart := time.Now().Add(24 * time.Hour)
	windowEnd := windowStart.Add(14 * 24 * time.Hour)
	busyPayload := []map[string]any{}
	if bl, ok := s.Calendar.(calendar.BusyLister); ok && len(allCandIDs) > 0 {
		intervals, berr := bl.ListBusy(ctx, windowStart, windowEnd, allCandIDs)
		if berr != nil {
			// non-fatal: agent can still assign without busy
			busyPayload = nil
		} else {
			for _, iv := range intervals {
				busyPayload = append(busyPayload, map[string]any{
					"open_id":   iv.OpenID,
					"starts_at": iv.StartsAt.Format(time.RFC3339),
					"ends_at":   iv.EndsAt.Format(time.RFC3339),
				})
			}
		}
	}

	durMin := int(duration / time.Minute)
	if durMin <= 0 {
		durMin = 60
	}

	resp, err := s.Agent.AssignScheduling(ctx, agentclient.SchedulingAssignRequest{
		ApplicationID: appID,
		RoundIndex:    roundIndex,
		JDRoundID:     jdRoundID,
		JDDepartment:  jdDept,
		DurationMin:   durMin,
		Requirements:  reqs,
		Candidates:    profiles,
		BusyIntervals: busyPayload,
		WindowStart:   windowStart.Format(time.RFC3339),
		WindowEnd:     windowEnd.Format(time.RFC3339),
	})
	if err != nil {
		return nil, nil, err
	}

	detail := map[string]any{"resolver": "scheduling_agent"}
	if resp.AssignmentDetail != nil {
		detail = resp.AssignmentDetail
	} else {
		detail["by_role"] = resp.ByRole
		detail["rationale"] = resp.Rationale
		detail["verify"] = resp.VerifyDetail
	}
	detail["jd_department"] = jdDept

	if resp.NeedsHuman {
		code := strings.TrimSpace(resp.HumanReasonCode)
		if code == "" {
			code = "scheduling_verify_failed"
		}
		msg := strings.TrimSpace(resp.Error)
		if msg == "" {
			msg = resp.Rationale
		}
		if msg == "" {
			msg = code
		}
		return nil, detail, fmt.Errorf("%s: %s", code, msg)
	}

	attendees := cleanOpenIDs(resp.AssignedOpenIDs)
	if len(attendees) == 0 {
		return nil, detail, fmt.Errorf("interviewers_unassigned: agent returned empty assignees")
	}
	detail["assigned_open_ids"] = attendees
	return attendees, detail, nil
}

func (s *Service) profilesForOpenIDs(ctx context.Context, openIDs []string) ([]map[string]any, error) {
	if len(openIDs) == 0 {
		return []map[string]any{}, nil
	}
	all, err := s.ListInterviewerProfiles(ctx, "", "")
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range openIDs {
		want[id] = true
	}
	var out []map[string]any
	for _, p := range all {
		oid := fmt.Sprint(p["open_id"])
		if want[oid] {
			out = append(out, p)
		}
	}
	return out, nil
}

func schedulingHumanCode(err error) string {
	if err == nil {
		return "interviewers_unassigned"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "interview_plan_missing"):
		return "interview_plan_missing"
	case strings.Contains(msg, "scheduling_verify_failed"),
		strings.Contains(msg, "scheduling_duplicate"),
		strings.Contains(msg, "scheduling_headcount"),
		strings.Contains(msg, "scheduling_role_"),
		strings.Contains(msg, "scheduling_department"),
		strings.Contains(msg, "scheduling_specialties"),
		strings.Contains(msg, "scheduling_cross_vendor"):
		// prefer leading code before ':'
		if i := strings.Index(msg, ":"); i > 0 {
			code := strings.TrimSpace(msg[:i])
			if strings.HasPrefix(code, "scheduling_") || code == "interviewers_unassigned" {
				return code
			}
		}
		return "scheduling_verify_failed"
	case strings.Contains(msg, "interviewers_unassigned"):
		return "interviewers_unassigned"
	default:
		return "interviewers_unassigned"
	}
}
