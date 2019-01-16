// Libraries
import React, {PureComponent} from 'react'
import {withRouter, WithRouterProps} from 'react-router'

// Components
import {Button, ComponentColor, ComponentSize} from 'src/clockface'
import {ErrorHandling} from 'src/shared/decorators/errors'

// Types
import {Organization} from 'src/api'

interface OwnProps {
  orgs: Organization[]
}

type Props = OwnProps & WithRouterProps

@ErrorHandling
class CompletionAdvancedButton extends PureComponent<Props> {
  public render() {
    return (
      <Button
        text="Advanced"
        color={ComponentColor.Success}
        size={ComponentSize.Large}
        onClick={this.handleAdvanced}
      />
    )
  }

  private handleAdvanced = (): void => {
    const {router, orgs} = this.props
    const id = orgs[0].id
    router.push(`/organizations/${id}/buckets_tab`)
  }
}

export default withRouter<OwnProps>(CompletionAdvancedButton)
