// Libraries
import React, {PureComponent} from 'react'
import {get} from 'lodash'

// Components
import {Context} from 'src/clockface'
import {ErrorHandling} from 'src/shared/decorators/errors'
import OverlayLink from 'src/overlays/components/OverlayLink'

// Types
import {IconFont, ComponentColor} from '@influxdata/clockface'
import {Cell, View} from 'src/types'

interface Props {
  cell: Cell
  view: View
  onDeleteCell: (cell: Cell) => void
  onCloneCell: (cell: Cell) => void
  onCSVDownload: () => void
  onEditCell: () => void
}

@ErrorHandling
export default class CellContext extends PureComponent<Props> {
  public render() {
    const {cell, onDeleteCell, onCloneCell} = this.props

    return (
      <Context className="cell--context">
        <Context.Menu icon={IconFont.Pencil}>{this.editMenuItems}</Context.Menu>
        <Context.Menu
          icon={IconFont.Duplicate}
          color={ComponentColor.Secondary}
        >
          <Context.Item label="Clone" action={onCloneCell} value={cell} />
        </Context.Menu>
        <Context.Menu icon={IconFont.Trash} color={ComponentColor.Danger}>
          <Context.Item label="Delete" action={onDeleteCell} value={cell} />
        </Context.Menu>
      </Context>
    )
  }

  private get editMenuItems(): JSX.Element[] | JSX.Element {
    const {view, onEditCell, onCSVDownload} = this.props

    if (view.properties.type === 'markdown') {
      return (
        <OverlayLink overlayID="edit-note" resourceID={view.id}>
          {onClick => (
            <Context.Item label="Edit Note" action={onClick} />
          )}
        </OverlayLink>
      )
    }

    const hasNote = !!get(view, 'properties.note')

    return [
      <Context.Item key="configure" label="Configure" action={onEditCell} />,
      <OverlayLink key="note" overlayID="edit-note" resourceID={view.id}>
        {onClick => (
          <Context.Item
            label={hasNote ? 'Edit Note' : 'Add Note'}
            action={onClick}
          />
        )}
      </OverlayLink>,
      <Context.Item
        key="download"
        label="Download CSV"
        action={onCSVDownload}
        disabled={true}
      />,
    ]
  }
}
