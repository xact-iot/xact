# Tag Calcs

Tag Calcs let you create **computed tags** - tags whose values are calculated automatically from other tags in the system. If you have ever written a formula in a spreadsheet, you already understand the core idea. Instead of typing a formula into a cell, you write an expression that reads live tag values and writes the result to a new tag on a schedule you choose.

## How Tag Calcs Work

A Tag Calc is a named formula that runs automatically in the background. Every time it runs it:

1. Reads the current values of whichever tags you reference in your expression.
2. Evaluates the expression (does the maths).
3. Writes the result to your chosen **output tag**.

The output tag is an ordinary tag - it shows up in the Tag Manager, can be displayed on dashboards, and can trigger notifications just like any other tag.

## When NOT to use Tag Calcs

Use a tag's input pipeline of processing blocks when working with individual tags. Tag Calcs are intended for aggregate operations. For example, when checking for a specific high limit on many similar devices, create a template device and add the limit check to the tag in question. Link all the similar devices to the template for easier management.

## Creating a Tag Calc

Open the **Tag Calcs** widget from the System category. Click **New** and fill in:

| Field | What it does |
|-------|--------------|
| **Name** | A human-readable label (e.g. "Total active devices") |
| **Description** | Optional - what does this script compute and why? |
| **Output Tag** | The dot-path of the tag that will receive the result (e.g. `VMS.KPI.total_active`) |
| **Expression** | The formula to evaluate |
| **Interval** | How often (in seconds) to run the formula |
| **Enabled** | Toggle the script on or off without deleting it |

## Tag References

To read the current value of a tag, simply write its path using dot notation:

```
VMS.Sign001.status.brightness
```

No extra symbols needed - just the dot-separated path. If a referenced tag does not exist or has no value, it is treated as **0**.

Tag paths reflect the tree hierarchy:

```
VMS.Sign001.status.brightness
|    |       |        |
|    |       |        └─ the tag name
|    |       └────────── the tag group
|    └────────────────── the device
└─────────────────────── the device type group
```

You do **not** include the organisation name - the system adds that automatically.

## Arithmetic

Standard arithmetic operators work exactly as in a spreadsheet:

| Operator | Meaning | Example |
|----------|---------|---------|
| `+` | Add | `VMS.Sign001.brightness + 10` |
| `-` | Subtract | `VMS.Sign001.brightness - VMS.Sign002.brightness` |
| `*` | Multiply | `Pump.flow * 60` |
| `/` | Divide | `Pump.totalVolume / Pump.runHours` |
| `%` | Remainder | `Pump.cycleCount % 24` |

**Operator precedence** follows normal rules (multiply and divide before add and subtract). Use parentheses to control the order:

```
(A.temp + B.temp) / 2       - average of two sensors
A.temp + B.temp / 2         - adds half of B to A (probably not what you want)
```

### Booleans

Boolean (true/false) tags are treated as numbers: **true = 1, false = 0**. This means you can add them together to count how many are true:

```
VMS.Sign001.status.online + VMS.Sign002.status.online + VMS.Sign003.status.online
```

## Wildcard Patterns

Typing every device path individually would be tedious when you have hundreds of devices. Wildcard patterns let you match many tags at once.

### The `*` Wildcard - matches one path segment

```
VMS.*.status.brightness        matches VMS.Sign001.status.brightness
                                       VMS.Sign002.status.brightness
                                       VMS.Sign099.status.brightness  ... and so on
```

`*` matches **exactly one** segment of the path. It will not cross a dot boundary.

### The `?` Wildcard - matches one character

```
VMS.Sign00?.brightness  matches VMS.Sign001.brightness through VMS.Sign009.brightness
                        but NOT VMS.Sign010.brightness
```

### Wildcards require aggregate functions

You cannot write `VMS.*.brightness` on its own - there is no single value to return. Instead, use one of the aggregate functions described below.

## Aggregate Functions

Aggregate functions take a wildcard pattern and reduce all matching tag values to a single number - just like `SUM()` or `AVERAGE()` in a spreadsheet.

### `avg(pattern)` - Average

Returns the mean of all matching tag values.

```
avg(VMS.*.brightness)          - average brightness across all signs
avg(Pumps.*.flowRate)          - average flow rate across all pumps
```

### `sum(pattern)` - Sum

Returns the total of all matching tag values.

```
sum(VMS.*.errorCount)          - total errors across all signs
sum(Meters.*.kWh)              - total energy consumption
```

### `min(pattern)` - Minimum

Returns the lowest value found.

```
min(Tanks.*.level)             - the lowest tank level in the system
```

### `max(pattern)` - Maximum

Returns the highest value found.

```
max(Pumps.*.motorTemp)         - the hottest motor temperature
```

### `count(pattern)` - Count

Returns the number of tags that match the pattern (regardless of their values).

```
count(VMS.*.meta.name)         - how many devices exist in the VMS group
```

### `countWhere(pattern, value)` - Count matching a value

Returns the number of tags that match the pattern **and** have a specific value. Accepts `true`/`false` as well as numbers.

```
countWhere(VMS.*.meta.online, true)              - how many devices are online
countWhere(VMS.*.meta.commonAlarmPresent, false)  - how many have no active alarms
countWhere(Pumps.*.stage, 2)                      - how many pumps are in stage 2
```

### `listHighest(pattern, count)` / `listLowest(pattern, count)` - Ranked device lists

Writes a sorted object array to the output tag instead of a single number. Each array element contains:

| Field | Meaning |
|-------|---------|
| `deviceName` | Name of the matched device |
| `deviceDescriptor` | Description stored on the matched device node |
| `tagName` | Device-relative tag path, such as `air.aqi` |
| `tagValue` | Numeric value used for ranking |

```
listHighest(LA_LongBeach.AirQuality.*.air.aqi, 5)  - top 5 AQI sensors
listLowest(LA_LongBeach.AirQuality.*.air.aqi, 5)   - lowest 5 AQI sensors
```

The output path is created as an array node if needed. Element entries are created automatically, and extra old elements are deleted when the requested count shrinks.

## Conditional Logic

### `if(condition, value_when_true, value_when_false)`

Works exactly like `IF()` in a spreadsheet:

```
if(Pump.running > 0, 1, 0)             - 1 if running, 0 if not
if(Tank.level < 20, 1, 0)              - alarm flag when level is low
```

### Comparison Operators

| Operator | Meaning | Example |
|----------|---------|---------|
| `==` | Equal to | `Pump.status == 1` |
| `!=` | Not equal to | `Pump.status != 0` |
| `>` | Greater than | `Tank.level > 80` |
| `<` | Less than | `Tank.level < 20` |
| `>=` | Greater than or equal | `Pump.runHours >= 1000` |
| `<=` | Less than or equal | `Pump.pressure <= 5.5` |

> **Note:** Use `==` (double equals) for comparison. A single `=` is not valid.

### Logical Operators

Combine multiple conditions:

| Operator | Meaning | Example |
|----------|---------|---------|
| `&&` | AND - both must be true | `Tank.level < 20 && Pump.running == 0` |
| `\|\|` | OR - either must be true | `Tank.level < 10 \|\| Tank.overflow == 1` |
| `!` | NOT - reverse true/false | `!Pump.running` |

### Nested Conditions

Put `if()` inside another `if()` to handle multiple cases:

```
if(Tank.level > 80, 2,
   if(Tank.level > 40, 1,
      0))
```

Returns 2 (high), 1 (normal), or 0 (low) depending on the tank level.

## Maths Functions

| Function | Description | Example |
|----------|-------------|---------|
| `abs(v)` | Absolute value | `abs(Sensor.deviation)` |
| `round(v, decimals)` | Round to decimal places | `round(avg(VMS.*.brightness), 0)` |
| `floor(v)` | Round down | `floor(Pump.runHours)` |
| `ceil(v)` | Round up | `ceil(Pump.runHours)` |
| `sqrt(v)` | Square root | `sqrt(sum(Sensors.*.squaredError))` |
| `pow(base, exp)` | Power | `pow(2, 8)` |
| `log(v)` | Natural logarithm | `log(Meter.reading)` |
| `log10(v)` | Base-10 logarithm | `log10(Meter.reading)` |
| `sin(v)`, `cos(v)`, `tan(v)` | Trigonometry (radians) | `sin(Rotor.angle)` |

## Worked Examples

### Example 1 - Count online devices

```
Name:        Online sign count
Output Tag:  VMS.KPI.online_count
Expression:  countWhere(VMS.*.meta.online, true)
Interval:    30
```

### Example 2 - Online percentage

Combine count functions to get a percentage:

```
Name:        Online percentage
Output Tag:  VMS.KPI.online_pct
Expression:  round(VMS.KPI.online_count / max(1, count(VMS.*.meta.online)) * 100, 1)
Interval:    60
```

`max(1, ...)` prevents division by zero - if there are no devices, the result is 0 rather than an error.

### Example 3 - Total energy consumption

```
Name:        Total site energy
Output Tag:  Site.KPI.totalKWh
Expression:  round(sum(Meters.*.kWh), 2)
Interval:    60
```

### Example 4 - System health score

Score 0-100 based on whether devices are online and alarm-free:

```
Name:        System health score
Output Tag:  VMS.KPI.health
Expression:  round(
               (countWhere(VMS.*.meta.online, true) * 50 +
                countWhere(VMS.*.meta.commonAlarmPresent, false) * 50)
               / max(1, count(VMS.*.meta.online)),
             1)
Interval:    60
```

Each device contributes 50 points for being online and 50 points for being alarm-free, averaged across the fleet.

### Example 5 - Alarm flag from a threshold

```
Name:        High temperature alarm
Output Tag:  Pumps.KPI.highTempAlarm
Expression:  if(avg(Pumps.*.motorTemp) > 85, 1, 0)
Interval:    15
```

### Example 6 - Phase total current

A simple sum of specific tags when wildcards are not appropriate:

```
Name:        Phase total current
Output Tag:  Feeder.KPI.totalCurrent
Expression:  Feeder.phaseA.current + Feeder.phaseB.current + Feeder.phaseC.current
Interval:    10
```

## Using the Test Button

Before enabling a script, always click **Test** to evaluate the expression immediately against live data. The result panel will show either:

- **Result: 42.5** - the expression evaluated successfully.
- **Error: ...** - a description of what went wrong.

Common reasons for a test to return 0 unexpectedly:

- The tag path is misspelled - check it in the Tag Manager
- The wildcard pattern does not match any tags - try a simpler pattern first
- The device is offline and its tags have no current value (treated as 0)

## Choosing an Interval

| Use case | Suggested interval |
|----------|--------------------|
| Real-time alarm flags | 10-15 seconds |
| Operational KPIs (counts, averages) | 30-60 seconds |
| Slow-moving totals (energy, volume) | 60-300 seconds |
| Summary / reporting values | 300+ seconds |

Short intervals for complex wildcard expressions over thousands of tags will use more server resources. Only use short intervals where genuinely needed.

## Common Mistakes

**Forgetting `==` for equality**
```
if(Pump.status = 1, ...)   - wrong, not valid
if(Pump.status == 1, ...)  - correct
```

**Dividing without zero protection**
```
Pump.output / Pump.input                    - returns 0 silently if input is 0
Pump.output / max(0.001, Pump.input)        - safe
```

**Using a wildcard without an aggregate**
```
VMS.*.brightness                    - not valid, which one?
avg(VMS.*.brightness)               - correct
```

**Output tag collides with a real device tag**

Choose an output tag path that is clearly a computed value:
```
VMS.KPI.online_count       - good, clearly derived
VMS.Sign001.online         - bad, this is a real tag
```

## Quick Reference

```
Tag reference:      NodeGroup.Device.tagname
Arithmetic:         A.tag + B.tag - C.tag * 2 / D.tag
Aggregates:         avg(G.*.tag)  sum(G.*.tag)  min(G.*.tag)  max(G.*.tag)
                    count(G.*.tag)  countWhere(G.*.tag, true)
Lists:              listHighest(G.*.tag, 5)  listLowest(G.*.tag, 5)
Conditional:        if(condition, value_if_true, value_if_false)
Comparison:         ==  !=  >  <  >=  <=
Logical:            &&  ||  !
Maths:              abs  round  floor  ceil  sqrt  pow  log  log10  sin  cos  tan
```
