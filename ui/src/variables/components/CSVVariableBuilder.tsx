import React, {PureComponent} from 'react'
import Papa from 'papaparse'
import _ from 'lodash'

// Component
import {Grid, Form, TextArea, Dropdown, Columns} from '@influxdata/clockface'

// Utils
import {ErrorHandling} from 'src/shared/decorators/errors'
import {trimAndRemoveQuotes} from 'src/variables/utils/mapBuilder'
import {pluralize} from 'src/shared/utils/pluralize'

interface Props {
  values: string[]
  onChange: (values: string[]) => void
  onSelectDefault: (selectedKey: string) => void
  selected?: string[]
}

interface State {
  csv: string
}

@ErrorHandling
export default class CSVTemplateBuilder extends PureComponent<Props, State> {
  state: State = {
    csv: this.props.values.map(value => `"${value}"`).join(',\n '),
  }

  public render() {
    const {onSelectDefault, values} = this.props
    const {csv} = this.state

    return (
      <Form.Element label="Comma Separated Values">
        <Grid.Row>
          <Grid.Column>
            <TextArea
              value={csv}
              onChange={this.handleChange}
              onBlur={this.handleBlur}
            />
          </Grid.Column>
        </Grid.Row>
        <Grid.Row>
          <Grid.Column widthXS={Columns.Six}>
            <p>
              CSV contains <strong>{values.length}</strong> value
              {pluralize(values)}
            </p>
          </Grid.Column>
          <Grid.Column widthXS={Columns.Six}>
            {
              <Form.Element label="Select A Default">
                <Dropdown
                  selectedID={this.defaultID}
                  onChange={onSelectDefault}
                  titleText="Values"
                >
                  {values.map(value => (
                    <Dropdown.Item key={value} id={value} value={value}>
                      {value}
                    </Dropdown.Item>
                  ))}
                </Dropdown>
              </Form.Element>
            }
          </Grid.Column>
        </Grid.Row>
      </Form.Element>
    )
  }

  private get defaultID(): string {
    const {selected, values} = this.props
    const firstEntry = _.get(values, '0', '')

    return _.get(selected, '0', firstEntry)
  }

  private handleBlur = (): void => {
    const {onChange} = this.props
    const {csv} = this.state

    const update = this.getValuesFromString(csv)

    onChange(update)
  }

  private handleChange = (csv: string): void => {
    this.setState({csv})
  }

  private getValuesFromString(csv: string) {
    const parsedTVS = Papa.parse(csv)
    const templateValuesData: string[][] = _.get(parsedTVS, 'data', [[]])

    const valueSet = new Set()
    for (const row of templateValuesData) {
      for (const value of row) {
        const trimmedValue = trimAndRemoveQuotes(value)

        if (trimmedValue !== '') {
          valueSet.add(trimmedValue)
        }
      }
    }

    return [...valueSet]
  }
}
