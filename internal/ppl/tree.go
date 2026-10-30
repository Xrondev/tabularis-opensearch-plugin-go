package ppl

import (
	"errors"
	"path"
	"strconv"
	"strings"
)

type parsedTree struct {
	Root  treeNode `json:"root"`
	Error string   `json:"error"`
}

type treeNode struct {
	Rule     string     `json:"rule"`
	Token    *int       `json:"token"`
	Text     string     `json:"text"`
	Children []treeNode `json:"children"`
}

var allowedRules = map[string]bool{
	"root": true, "pplStatement": true, "dmlStatement": true, "queryStatement": true,
	"pplCommands": true, "commands": true, "searchCommand": true, "whereCommand": true,
	"fieldsCommand": true, "statsCommand": true, "sortCommand": true, "headCommand": true,
	"fromClause": true, "tableSourceClause": true, "tableSource": true, "tableQualifiedName": true,
	"tableIdent": true, "ident": true, "keywordsCanBeId": true, "comparisonExpression": true,
	"comparisonOperator": true, "logicalExpression": true, "valueExpression": true,
	"primaryExpression": true, "fieldExpression": true, "qualifiedName": true, "fieldList": true,
	"literalValue": true, "integerLiteral": true, "decimalLiteral": true, "stringLiteral": true,
	"booleanLiteral": true, "sortField": true, "sortFieldExpression": true, "sortbyClause": true,
	"statsAggTerm": true, "statsByClause": true, "statsFunction": true, "statsFunctionName": true,
}

type treeInfo struct {
	index   string
	head    int
	hasHead bool
}

func validateTree(root treeNode, scope string, maxHead int) (treeInfo, error) {
	if root.Rule != "root" {
		return treeInfo{}, errors.New("PPL parse tree has no root")
	}
	var sources []string
	var lastHead int
	hasHead := false
	fromClauses, searchCommands, nodes := 0, 0, 0
	var visit func(treeNode, int) error
	visit = func(node treeNode, depth int) error {
		nodes++
		if nodes > 5000 || depth > 256 {
			return errors.New("PPL parse tree exceeds its resource limit")
		}
		if node.Token != nil {
			if node.Rule != "" || len(node.Children) != 0 {
				return errors.New("invalid PPL token")
			}
			if *node.Token == 333 || *node.Token == 334 {
				if len(node.Text) < 2 || strings.Contains(node.Text, "\\") || strings.Contains(node.Text[1:len(node.Text)-1], node.Text[:1]) {
					return errors.New("escaped PPL string literals are not supported")
				}
			}
			return nil
		}
		if !allowedRules[node.Rule] {
			return errors.New("PPL contains an unsupported rule")
		}
		if node.Rule == "fromClause" {
			fromClauses++
		}
		if node.Rule == "searchCommand" {
			searchCommands++
		}
		if node.Rule == "headCommand" {
			hasHead = true
			lastHead = 10
			afterFrom := false
			sizeSeen := false
			offsetSeen := false
			for _, child := range node.Children {
				if child.Token != nil && strings.EqualFold(child.Text, "from") {
					afterFrom = true
					continue
				}
				if child.Rule != "integerLiteral" {
					continue
				}
				if len(child.Children) != 1 || child.Children[0].Token == nil {
					return errors.New("invalid PPL head argument")
				}
				value, err := strconv.Atoi(child.Children[0].Text)
				if err != nil || value < 0 {
					return errors.New("invalid PPL head argument")
				}
				if afterFrom {
					if offsetSeen || value > 10000 {
						return errors.New("PPL head offset exceeds the supported bound")
					}
					offsetSeen = true
				} else {
					if sizeSeen {
						return errors.New("invalid PPL head size")
					}
					sizeSeen = true
					lastHead = value
				}
			}
			if lastHead > maxHead {
				return errors.New("PPL head exceeds the supported result limit")
			}
		}
		if node.Rule == "tableSource" {
			var leaves []treeNode
			var collect func(treeNode)
			collect = func(n treeNode) {
				if n.Token != nil {
					leaves = append(leaves, n)
				}
				for _, child := range n.Children {
					collect(child)
				}
			}
			collect(node)
			if len(leaves) != 1 || (*leaves[0].Token != 328 && *leaves[0].Token != 332) {
				return errors.New("quoted or qualified PPL sources are not supported")
			}
			sources = append(sources, leaves[0].Text)
		}
		for _, child := range node.Children {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root, 0); err != nil {
		return treeInfo{}, err
	}
	if len(sources) != 1 || fromClauses != 1 || searchCommands != 1 || !scopeMatches(sources[0], scope) {
		return treeInfo{}, errors.New("PPL source is outside the configured index scope")
	}
	return treeInfo{index: sources[0], head: lastHead, hasHead: hasHead}, nil
}

func scopeMatches(index, scope string) bool {
	if index == "" || strings.Contains(index, "..") {
		return false
	}
	for _, r := range index {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == '*') {
			return false
		}
	}
	for _, allowed := range strings.Split(scope, ",") {
		if index == allowed {
			return true
		}
		if strings.Contains(index, "*") {
			continue
		}
		matched, err := path.Match(allowed, index)
		if err == nil && matched {
			return true
		}
	}
	return false
}
