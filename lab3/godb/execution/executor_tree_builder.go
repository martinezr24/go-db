package execution

import (
	"fmt"

	"mit.edu/dsg/godb/catalog"
	"mit.edu/dsg/godb/indexing"
	"mit.edu/dsg/godb/planner"
)

// BuildExecutorTree compiles a Physical Plan tree into an executable Executor tree.
func BuildExecutorTree(
	plan planner.PlanNode,
	cat *catalog.Catalog,
	tm *TableManager,
	im *indexing.IndexManager,
) (Executor, error) {

	if plan == nil {
		return nil, fmt.Errorf("cannot build executor from nil plan node")
	}

	switch node := plan.(type) {

	case *planner.SeqScanNode:
		tableHeap, err := tm.GetTable(node.TableOid)
		if err != nil {
			return nil, fmt.Errorf("SeqScan: failed to get table %d: %v", node.TableOid, err)
		}
		return NewSeqScanExecutor(node, tableHeap), nil

	case *planner.IndexScanNode:
		index, err := im.GetIndex(node.IndexOid)
		if err != nil {
			return nil, fmt.Errorf("IndexScan: failed to get index %d: %v", node.IndexOid, err)
		}
		tableHeap, err := tm.GetTable(node.TableOid)
		if err != nil {
			return nil, fmt.Errorf("IndexScan: failed to get table %d: %v", node.TableOid, err)
		}
		err = node.BindKey(index)
		if err != nil {
			return nil, fmt.Errorf("IndexScan: failed to bind index key for index %d: %v", node.IndexOid, err)
		}
		return NewIndexScanExecutor(node, index, tableHeap), nil

	case *planner.IndexLookupNode:
		index, err := im.GetIndex(node.IndexOid)
		if err != nil {
			return nil, fmt.Errorf("IndexLookup: failed to get index %d: %v", node.IndexOid, err)
		}
		tableHeap, err := tm.GetTable(node.TableOid)
		if err != nil {
			return nil, fmt.Errorf("IndexLookup: failed to get table %d: %v", node.TableOid, err)
		}
		err = node.BindKey(index)
		if err != nil {
			return nil, fmt.Errorf("IndexLookup: failed to bind index key for index %d: %v", node.IndexOid, err)
		}
		return NewIndexLookupExecutor(node, index, tableHeap), nil

	case *planner.ValuesNode:
		return NewValuesExecutor(node), nil

	case *planner.InsertNode:
		childExec, err := BuildExecutorTree(node.Child, cat, tm, im)
		if err != nil {
			return nil, err
		}

		targetTable, err := tm.GetTable(node.TableOid)
		if err != nil {
			return nil, err
		}

		tableMetadata, err := cat.GetTableByOid(node.TableOid)
		if err != nil {
			return nil, fmt.Errorf("Insert: failed to get metadata for table %d: %v", node.TableOid, err)
		}
		indexes := make([]indexing.Index, len(tableMetadata.Indexes))
		for i, idxMeta := range tableMetadata.Indexes {
			indexes[i], err = im.GetIndex(idxMeta.Oid)
			if err != nil {
				return nil, fmt.Errorf("Insert: failed to get index %d for table %d: %v", idxMeta.Oid, node.TableOid, err)
			}
		}

		return NewInsertExecutor(node, childExec, targetTable, indexes), nil

	case *planner.DeleteNode:
		childExec, err := BuildExecutorTree(node.Child, cat, tm, im)
		if err != nil {
			return nil, err
		}

		targetTable, err := tm.GetTable(node.TableOid)
		if err != nil {
			return nil, err
		}

		tableMetadata, err := cat.GetTableByOid(node.TableOid)
		if err != nil {
			return nil, fmt.Errorf("Delete: failed to get metadata for table %d: %v", node.TableOid, err)
		}
		indexes := make([]indexing.Index, len(tableMetadata.Indexes))
		for i, idxMeta := range tableMetadata.Indexes {
			indexes[i], err = im.GetIndex(idxMeta.Oid)
			if err != nil {
				return nil, fmt.Errorf("Delete: failed to get index %d for table %d: %v", idxMeta.Oid, node.TableOid, err)
			}
		}

		return NewDeleteExecutor(node, childExec, targetTable, indexes), nil

	case *planner.UpdateNode:
		childExec, err := BuildExecutorTree(node.Child, cat, tm, im)
		if err != nil {
			return nil, err
		}

		targetTable, err := tm.GetTable(node.TableOid)
		if err != nil {
			return nil, err
		}

		tableMetadata, err := cat.GetTableByOid(node.TableOid)
		if err != nil {
			return nil, fmt.Errorf("Update: failed to get metadata for table %d: %v", node.TableOid, err)
		}
		indexes := make([]indexing.Index, len(tableMetadata.Indexes))
		for i, idxMeta := range tableMetadata.Indexes {
			indexes[i], err = im.GetIndex(idxMeta.Oid)
			if err != nil {
				return nil, fmt.Errorf("Update: failed to get index %d for table %d: %v", idxMeta.Oid, node.TableOid, err)
			}
		}

		return NewUpdateExecutor(node, childExec, targetTable, indexes), nil

	case *planner.NestedLoopJoinNode:
		leftExec, err := BuildExecutorTree(node.Left, cat, tm, im)
		if err != nil {
			return nil, err
		}
		rightExec, err := BuildExecutorTree(node.Right, cat, tm, im)
		if err != nil {
			return nil, err
		}

		return NewBlockNestedLoopJoinExecutor(node, leftExec, rightExec), nil

	case *planner.IndexNestedLoopJoinNode:
		leftExec, err := BuildExecutorTree(node.Left, cat, tm, im)
		if err != nil {
			return nil, err
		}

		rightIndex, err := im.GetIndex(node.RightIndexOid)
		if err != nil {
			return nil, fmt.Errorf("INLJ: failed to resolve index %d: %v", node.RightIndexOid, err)
		}
		rightTable, err := tm.GetTable(node.RightTableOid)
		if err != nil {
			return nil, fmt.Errorf("INLJ: failed to resolve table %d for index %d: %v", node.RightTableOid, node.RightIndexOid, err)
		}
		tableMetadata, err := cat.GetTableByOid(node.RightTableOid)
		if err != nil {
			return nil, fmt.Errorf("INLJ: failed to get metadata for table %d: %v", node.RightTableOid, err)
		}
		// Ensure the index belongs to the right table
		indexBelongsToTable := false
		for _, idxMeta := range tableMetadata.Indexes {
			if idxMeta.Oid == node.RightIndexOid {
				indexBelongsToTable = true
				break
			}
		}
		if !indexBelongsToTable {
			return nil, fmt.Errorf("INLJ: index %d does not belong to table %d", node.RightIndexOid, node.RightTableOid)
		}

		return NewIndexJoinExecutor(node, leftExec, rightIndex, rightTable), nil

	case *planner.HashJoinNode:
		leftExec, err := BuildExecutorTree(node.Left, cat, tm, im)
		if err != nil {
			return nil, err
		}
		rightExec, err := BuildExecutorTree(node.Right, cat, tm, im)
		if err != nil {
			return nil, err
		}

		return NewHashJoinExecutor(node, leftExec, rightExec), nil

	case *planner.SortMergeJoinNode:
		leftExec, err := BuildExecutorTree(node.Left, cat, tm, im)
		if err != nil {
			return nil, err
		}
		rightExec, err := BuildExecutorTree(node.Right, cat, tm, im)
		if err != nil {
			return nil, err
		}

		return NewSortMergeJoinExecutor(node, leftExec, rightExec), nil

	case *planner.FilterNode:
		childExec, err := BuildExecutorTree(node.Child, cat, tm, im)
		if err != nil {
			return nil, err
		}
		return NewFilter(node, childExec), nil

	case *planner.ProjectionNode:
		childExec, err := BuildExecutorTree(node.Child, cat, tm, im)
		if err != nil {
			return nil, err
		}
		return NewProjectionExecutor(node, childExec), nil

	case *planner.LimitNode:
		childExec, err := BuildExecutorTree(node.Child, cat, tm, im)
		if err != nil {
			return nil, err
		}
		return NewLimitExecutor(node, childExec), nil

	case *planner.SortNode:
		childExec, err := BuildExecutorTree(node.Child, cat, tm, im)
		if err != nil {
			return nil, err
		}
		return NewSortExecutor(node, childExec), nil

	case *planner.TopNNode:
		childExec, err := BuildExecutorTree(node.Child, cat, tm, im)
		if err != nil {
			return nil, err
		}
		return NewTopNExecutor(node, childExec), nil

	case *planner.AggregateNode:
		childExec, err := BuildExecutorTree(node.Child, cat, tm, im)
		if err != nil {
			return nil, err
		}
		return NewAggregateExecutor(node, childExec), nil
	case *planner.MaterializeNode:
		childExec, err := BuildExecutorTree(node.Child, cat, tm, im)
		if err != nil {
			return nil, err
		}
		return NewMaterializeExecutor(node, childExec), nil

	default:
		return nil, fmt.Errorf("BuildExecutorTree: unsupported plan node type %T", node)
	}
}
