// Copyright 2025 Scalytics, Inc. and Scalytics Europe, LTD
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kafka

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"kaf-mirror/internal/config"
	"kaf-mirror/pkg/logger"
)

const topicRegexChars = `\^$*+?()[]{}|`

func IsTopicRegex(pattern string) bool {
	return strings.ContainsAny(pattern, topicRegexChars)
}

type TopicRule struct {
	source string
	target string
	regex  *regexp.Regexp
}

func NewTopicRule(source, target string) (TopicRule, error) {
	if !IsTopicRegex(source) {
		if target == "" {
			target = source
		}
		return TopicRule{source: source, target: target}, nil
	}
	re, err := regexp.Compile("^(?:" + source + ")$")
	if err != nil {
		return TopicRule{}, fmt.Errorf("invalid regex pattern %q: %w", source, err)
	}
	if target == "" {
		target = "$0"
	}
	return TopicRule{source: source, target: target, regex: re}, nil
}

func (r TopicRule) Source() string {
	return r.source
}

func (r TopicRule) IsRegex() bool {
	return r.regex != nil
}

func (r TopicRule) TargetFor(topic string) (string, bool) {
	if r.regex == nil {
		if topic != r.source {
			return "", false
		}
		return r.target, true
	}
	if !r.regex.MatchString(topic) {
		return "", false
	}
	return r.regex.ReplaceAllString(topic, r.target), true
}

type TopicRules struct {
	exact []TopicRule
	regex []TopicRule
}

func NewTopicRules(mappings []config.TopicMapping) (TopicRules, error) {
	var rules TopicRules
	for _, m := range mappings {
		if !m.Enabled {
			continue
		}
		rule, err := NewTopicRule(m.Source, m.Target)
		if err != nil {
			return TopicRules{}, err
		}
		if err := rules.add(rule); err != nil {
			return TopicRules{}, err
		}
	}
	return rules, nil
}

func (rs *TopicRules) add(rule TopicRule) error {
	if rule.IsRegex() {
		rs.regex = append(rs.regex, rule)
		return nil
	}
	for _, existing := range rs.exact {
		if existing.source == rule.source && existing.target != rule.target {
			return fmt.Errorf("topic %s matches multiple targets: %s and %s", rule.source, existing.target, rule.target)
		}
	}
	rs.exact = append(rs.exact, rule)
	return nil
}

func (rs TopicRules) HasRegex() bool {
	return len(rs.regex) > 0
}

func (rs TopicRules) RegexTargetFor(topic string) (string, bool, error) {
	target, found := "", false
	for _, rule := range rs.regex {
		t, ok := rule.TargetFor(topic)
		if !ok {
			continue
		}
		if found && t != target {
			return "", false, fmt.Errorf("topic %s matches multiple targets: %s and %s", topic, target, t)
		}
		target, found = t, true
	}
	if found && target == "" {
		return "", false, fmt.Errorf("regex mapping for %s produced empty target topic", topic)
	}
	return target, found, nil
}

func (rs TopicRules) Resolve(sourceTopics []string) ([]string, map[string]string, error) {
	topics := make([]string, 0, len(rs.exact))
	topicMap := make(map[string]string)
	targetToSource := make(map[string]string)
	for _, rule := range rs.exact {
		if _, ok := topicMap[rule.source]; ok {
			continue
		}
		topics = append(topics, rule.source)
		topicMap[rule.source] = rule.target
		targetToSource[rule.target] = rule.source
	}
	if !rs.HasRegex() {
		return topics, topicMap, nil
	}

	sorted := append([]string(nil), sourceTopics...)
	sort.Strings(sorted)
	matched := make([]bool, len(rs.regex))
	for _, topic := range sorted {
		for i, rule := range rs.regex {
			if _, ok := rule.TargetFor(topic); ok {
				matched[i] = true
			}
		}
		if _, ok := topicMap[topic]; ok {
			continue
		}
		target, ok, err := rs.RegexTargetFor(topic)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}
		if existing, ok := targetToSource[target]; ok {
			return nil, nil, fmt.Errorf("target topic %s is mapped from multiple sources: %s and %s", target, existing, topic)
		}
		topics = append(topics, topic)
		topicMap[topic] = target
		targetToSource[target] = topic
	}
	for i, rule := range rs.regex {
		if !matched[i] {
			logger.Warn("Regex mapping %s did not match any source topics", rule.source)
		}
	}
	return topics, topicMap, nil
}

type SourceTopicLister interface {
	GetClusterInfo(ctx context.Context) (*ClusterInfo, error)
}

func ResolveTopics(ctx context.Context, source SourceTopicLister, mappings []config.TopicMapping) ([]string, map[string]string, error) {
	rules, err := NewTopicRules(mappings)
	if err != nil {
		return nil, nil, err
	}
	if !rules.HasRegex() {
		return rules.Resolve(nil)
	}
	info, err := source.GetClusterInfo(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list source topics for regex mapping: %w", err)
	}
	names := make([]string, 0, len(info.Topics))
	for name := range info.Topics {
		names = append(names, name)
	}
	return rules.Resolve(names)
}
