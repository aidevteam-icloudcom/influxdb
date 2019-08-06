// Libraries
import React, {FC, useReducer, Dispatch} from 'react'
import {withRouter, WithRouterProps} from 'react-router'

// Components
import RuleSchedule from 'src/alerting/components/notifications/RuleSchedule'
import RuleConditions from 'src/alerting/components/notifications/RuleConditions'
import RuleMessage from 'src/alerting/components/notifications/RuleMessage'
import {Overlay, Form, Input, Grid} from '@influxdata/clockface'

// Reducers
import {reducer, RuleState, Actions} from './NewRuleOverlay.reducer'

// Constants
import {newRule, endpoints} from 'src/alerting/constants'

// Types
import {NotificationRuleBox} from 'src/types'

type Props = WithRouterProps

export const newRuleState: RuleState = {
  ...newRule,
  schedule: 'every',
}

export const NewRuleDispatch = React.createContext<Dispatch<Actions>>(null)

const NewRuleOverlay: FC<Props> = ({params, router}) => {
  const handleDismiss = () => {
    router.push(`/orgs/${params.orgID}/alerting`)
  }

  const [rule, dispatch] = useReducer(reducer, newRuleState)

  const handleChange = e => {
    const {name, value} = e.target
    dispatch({
      type: 'UPDATE_RULE',
      rule: {...rule, [name]: value} as NotificationRuleBox,
    })
  }

  return (
    <NewRuleDispatch.Provider value={dispatch}>
      <Overlay visible={true}>
        <Overlay.Container maxWidth={1000} className="rule-eo">
          <Overlay.Header
            title="Create a Notification Rule"
            onDismiss={handleDismiss}
          />
          <Overlay.Body>
            <Grid>
              <Form>
                <Grid.Row>
                  <Grid.Column>
                    <Form.Element label="Name">
                      <Input
                        testID="rule-name--input"
                        placeholder="Name this new rule"
                        value={rule.name}
                        name="name"
                        onChange={handleChange}
                      />
                    </Form.Element>
                  </Grid.Column>
                </Grid.Row>
                <RuleSchedule rule={rule} onChange={handleChange} />
                <RuleConditions rule={rule} />
                <RuleMessage rule={rule} endpoints={endpoints} />
              </Form>
            </Grid>
          </Overlay.Body>
        </Overlay.Container>
      </Overlay>
    </NewRuleDispatch.Provider>
  )
}

export default withRouter<Props>(NewRuleOverlay)
