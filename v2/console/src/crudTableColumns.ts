export const CRUD_SELECTION_COLUMN_WIDTH = 52
export const CRUD_PRIMARY_COLUMN_WIDTH = 280

export const crudSelectionStickyColumn = {
  sticky: 'left',
  stickyOffset: 0,
  width: CRUD_SELECTION_COLUMN_WIDTH,
  minWidth: CRUD_SELECTION_COLUMN_WIDTH,
  maxWidth: CRUD_SELECTION_COLUMN_WIDTH,
} as const

export const crudPrimaryStickyColumn = {
  sticky: 'left',
  stickyOffset: CRUD_SELECTION_COLUMN_WIDTH,
  width: CRUD_PRIMARY_COLUMN_WIDTH,
  minWidth: CRUD_PRIMARY_COLUMN_WIDTH,
  maxWidth: CRUD_PRIMARY_COLUMN_WIDTH,
} as const
