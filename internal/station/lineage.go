package station

import (
	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

// materialNode 把一级标本和子样本统一为可取样的库存节点。
// 两种实体都携带“初始/剩余/已去除”数量字段，去除规则因此完全一致。
type materialNode struct {
	id         string
	subject    domain.TerminationSubject
	initial    int
	remaining  int
	released   bool
	terminated bool
	specimen   *domain.Specimen
	subsample  *domain.Subsample
}

func nodeFromSpecimen(value domain.Specimen) materialNode {
	copied := value
	return materialNode{
		id:         value.ID,
		subject:    domain.TerminationOfSpecimen,
		initial:    value.InitialQuantity,
		remaining:  value.RemainingUnits,
		released:   value.IsReleased(),
		terminated: value.IsTerminated(),
		specimen:   &copied,
	}
}

func nodeFromSubsample(value domain.Subsample) materialNode {
	copied := value
	return materialNode{
		id:         value.ID,
		subject:    domain.TerminationOfSubsample,
		initial:    value.InitialQuantity,
		remaining:  value.RemainingUnits,
		terminated: value.Terminated,
		subsample:  &copied,
	}
}

// sealed 表示该母对象已放行或已终止，不允许任何新增去除。
func (n materialNode) sealed() bool {
	return n.released || n.terminated
}

// commit 把节点上的库存扣减写回对应的聚合实体。
func (n *materialNode) commit(m *repository.Mutation) {
	if n.specimen != nil {
		m.PutSpecimen(*n.specimen)
		return
	}
	if n.subsample != nil {
		m.PutSubsample(*n.subsample)
	}
}

// resolveMaterialNode 在同一次事务内解析一个库存对象。
// 标本与子样本共享调用方看到的标识空间；若极端情况下标识在两个集合中
// 同时存在，返回冲突而不是任选其一，避免把去除记到错误母对象上。
func resolveMaterialNode(m *repository.Mutation, id string) (materialNode, error) {
	specimen, specimenOK := m.GetSpecimen(id)
	subsample, subsampleOK := m.GetSubsample(id)
	if specimenOK && subsampleOK {
		return materialNode{}, contracts.Conflict("ambiguous_material_id", "标识同时命中标本与子样本，无法确定母对象")
	}
	if specimenOK {
		return nodeFromSpecimen(specimen), nil
	}
	if subsampleOK {
		return nodeFromSubsample(subsample), nil
	}
	return materialNode{}, contracts.NotFound("material_not_found", "未找到标本或子样本 "+id)
}

// lineage 描述一个母对象的溯源链：根标识在前、母对象自身在最后。
type lineage struct {
	chain []string
	depth int
}

// resolveLineage 沿 ParentID 向上遍历。
// 断链返回 not_found，重复访问返回冲突，二者都阻止建立会产生孤立记录的子样本。
func resolveLineage(m *repository.Mutation, node materialNode) (lineage, error) {
	visited := map[string]bool{node.id: true}
	order := []string{node.id}

	current := node
	for current.subsample != nil {
		parentID := current.subsample.ParentID
		if visited[parentID] {
			return lineage{}, contracts.Conflict("subsample_cycle", "父子链路出现循环，不能登记子样本")
		}
		parent, err := resolveMaterialNode(m, parentID)
		if err != nil {
			return lineage{}, err
		}
		visited[parentID] = true
		order = append(order, parentID)
		current = parent
	}

	// order 当前为“母对象 -> … -> 根”，反转为根在前。
	for left, right := 0, len(order)-1; left < right; left, right = left+1, right-1 {
		order[left], order[right] = order[right], order[left]
	}
	return lineage{chain: order, depth: len(order)}, nil
}
