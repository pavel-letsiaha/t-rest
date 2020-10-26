![Bozr](https://raw.githubusercontent.com/kajf/bozr/master/assets/bozr.png)

Minimalistic tool to perform API tests based on JSON description

[![Build Status](https://travis-ci.org/kajf/bozr.svg?branch=master)](https://travis-ci.org/kajf/bozr?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/kajf/bozr)](https://goreportcard.com/report/github.com/kajf/bozr)

## Usage

```bash
bozr [OPTIONS] (DIR|FILE)

Options:
  -H, --host      Base URL prefix for test calls
  -w, --workers   Execute in parallel with specified number of workers
      --throttle  Execute no more than specified number of requests per second (in suite)
  -h, --help      Print usage
  -i, --info      Enable info mode. Print request and response details.
  -d, --debug     Enable debug mode
      --junit     Enable junit xml reporter
  -v, --version   Print version information and quit

Examples:
  bozr ./examples/suite-file.suite.json
  bozr -w 2 ./examples
  bozr -H http://example.com ./examples
```

Usage [demo](https://asciinema.org/a/85699)

## Installation

Download the [latest binary release](https://github.com/kajf/bozr/releases) and unpack it.

## Test Suite Format

Test suite (suite_name.suite.json)

    ├ Test A [test case]
    |   ├ name
    |   ├ ignore [ignore test due to a specified reason]
    |   ├ args [value(s) for placeholders to use in request params, headers or body]
    │   ├ Call one
    |   |   ├ args 
    │   │   ├ on [single http request]
    │   │   ├ expect [http response asserts: code, headers, body, schema, etc.]
    │   │   └ remember [optionally remember variable(s) for the next call to use in request params, headers or body]
    │   └ Call two
    |       ├ args
    │       ├ on
    │       ├ expect
    │       └ remember
    └ Test B
        ├ name
        └ Call one
            ├ args
            ├ on
            ├ expect
            └ remember
         

### Suite file extension

All suites must have `.suite.json` extension.

If you want to temporary disable suite, change extension to `.xsuite.json`. Bozr does not execute ignored suites, but reports all test cases as skipped.

### Section 'On'

Represents http request parameters

```json
{
  "on": {
    "method": "POST",
    "url": "/api/company/users",
    "headers": {
        "Accept": "application/json",
        "Content-Type": "application/json"
    },
    "params": {
        "role": "admin"
    },
    "bodyFile" : "admins.json"
  }
}
```

| Field    | Description                                                          |
| -------- | -------------------------------------------------------------------- |
| method   | HTTP method                                                          |
| url      | HTTP request URL                                                     |
| headers  | HTTP request headers                                                 |
| params   | HTTP query params                                                    |
| bodyFile | File to send as a request payload (path relative to test suite json) |
| body     | String or JSON object to send as a request payload                   |

### Section 'Expect'

Represents assertions for http response of the test call.

Response:

```json
{
  "errors": [{ "code": "FOO" }]
}
```

Passing Test:

```json
{
  "expect": {
    "statusCode": 200,
    "contentType": "application/json",
    "bodyPath": {
        "errors.size()": 1
    }
  }
}
```

| Assertion      | Description                                                                              | Example                                         |
| -------------- | ---------------------------------------------------------------------------------------- | ----------------------------------------------- |
| statusCode     | Expected http response header 'Status Code'                                              | 200                                             |
| contentType    | Expected http response 'Content-Type'                                                    | application/json                                |
| bodySchemaFile | Path to json schema to validate response body (path relative to test suite file) | login-schema.json                               |
| bodySchemaURI  | URI to json schema to validate response body (absolute or relative to the host)  | http://example.com/api/scheme/login-schema.json |
| bodySchema     | Embedded json schema to validate response body                                           | { "type": "object", "required": [ "field_name" ]
| body           | Expected body structure and values. Not strict, e.g. full equality is not required. Response may contain more properties. But all specified must match.                                                      |
| exactBody           | Expected exact body structure and values. Specified body should fully match response. Not specified properties returned in response will cause error.                                                       |
| bodyPath           | Body matchers: equals, search, size                                                      |
| absent         | Paths that are NOT expected to be in response                                            | ['user.cardNumber', 'user.password']            |
| present        | Paths that are expected to be in response                                                | ['user.cardNumber', 'user.password']            |
| headers        | Expected http headers, specified as a key-value pairs.                                   |

#### 'Expect' body matchers

Response:

```json
{
  "users": [
    {"name":"John", "surname":"Wayne", "age": 38},
    {"name":"John", "surname":"Doe", "age": 12}
  ],
  "errors": []
}
```

Could be used to partially match response body:

```json
{
  "expect": {
    "body": {
      "users": [
        {"name":"John", "age": 38}
      ]
    }
  }
}
```

Exact match (no new properties in the response) can be checked using "exactBody".


#### 'Expect' body path matchers

Response:

```json
{
  "users": [
    {"name":"John", "surname":"Wayne", "age": 38},
    {"name":"John", "surname":"Doe", "age": 12}
  ],
  "errors": []
}
```

Passing Test `expect` fragment:

```json
{
  "expect": {
    "bodyPath": {
        "users.1.surname" : "Doe",
        "users.name":"John",
        "users": {
          "name":"John",
          "age": 12
        },
        "errors.size()": 0
    }
  }
}
```

| Type   | Assertion                                                                             | Example                                               |
| ------ | ------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| equals | Root 'users' array zero element has value of 'id' equal to '123'                      | "users.0.id" : "123"                                  |
| search | Root 'users' array contains element(s) with 'name' equal to 'Jack' or 'Dan' and 'Ron' | "users.name" : "Jack" or "users.name" : ["Dan","Ron"] |
| size   | Root 'company' element has 'users' array with '22' elements within 'buildings' array  | "company.buildings.users.size()" : 22                 |

XML:

- To match attribute use `-` symbol before attribute name. E.g. `users.0.-id`
- Namespaces are ignored
- Only string matcher values are supported (since xml has no real data types, so everything is a string)

#### 'Expect absent' body matchers

Represents paths not expected to be in response body.
Mostly used for security checks (e.g. returned user object should not contain password or credit card number fields)
Path fromat is the same as in `expect.bodyPath` section

```json
{
  "expect": {
    "absent": ["user.cardNumber", "user.password"]
  }
}
```

#### 'Expect present' body matchers

Represents paths expected to be in response body.
Should be used **only** for checks when particular value **is not known**. 
Path fromat is the same as in `expect.bodyPath` section

```json
{
  "expect": {
    "present": ["user.cardNumber", "user.password"]
  }
}
```

### Section 'Args'

Specifies placeholder values for future reference (within test scope)

Placeholder values could be used inside `on.url`, `on.params`, `on.headers`, `on.body`, `on.bodyFile`, `expect.headers`, `expect.body`, `expect.bodyPath` sections.

```json
{
  "args": {
    "currencyCode" : "USD",
    "magicNumber" : "12f"
  }
}
```

Given `args` are defined like above, placeholders {currencyCode} and {magicNumber} may be used in correspondent test case.

example_bodyfile.json

```json
{
  "bankAccount": {
    "currency": "{currencyCode}",
    "amount": 1000,
    "secret": "{magicNumber}"
  }
}
```

Resulting data will contain "USD" and "12f" values instead of placeholders.

```json
{
  "bankAccount": {
    "currency": "USD",
    "amount": 1000,
    "secret": "12f"
  }
}
```

```json
{
  "on": {
    "method": "GET",
    "url": "{hateoas_reference}",
    "headers": {
      "X-Secret-Key": "{secret_key}"
    }
  }
}
```
__Duplicated or unused argements are reported as test failure__

### Functions and data generation

#### Hashes

_.SHA1_ calculates SHA-1 hash of it's argument

```json
{
  "hash": "{{ .SHA1 `Username` }}"
}
```

_.Base64_ does [Base64](https://en.wikipedia.org/wiki/Base64) transformation on provided string

```json
{
  "encoded": "{{ .Base64 `some value` }}"
}
```

#### Date and time

_.Now_ returns current date/time

```json
{
  "currentDate": "{{ .Now | .FormatDateTime `2006-01-02` }}"
}
```

In example above 'currentDate' argument will have value of current date in yyyy-mm-dd format

It is also possible to specify IANA timezone

```json
{
  "currentDateInNY": "{{ `America/New_York` | .Now }}"
}
```

_.DaysFromNow_ returns date/time that is N days from now

```json
{
  "yesterday": "{{-1 | .DaysFromNow | .FormatDateTime `2006-01-02` }}"
}
```

In example above 'yesterday' argument will have value of yesterday's date in yyyy-mm-dd format

_.FormatDateTime_ returns string representation of date/time (useful in combination with _.Now_ or _.DaysFromNow_)

Function uses example-based format (constant date '2006-01-02T15:04:05Z07:00' used as example instead of pattern)

_.CurrentTimestampSec_ returns number representing current date/time in [Unix format](https://en.wikipedia.org/wiki/Unix_time)

#### SOAP

_.WSSEPasswordDigest_ calculates password digest according to [Web Service Security specification](https://www.oasis-open.org/committees/download.php/13392/wss-v1.1-spec-pr-UsernameTokenProfile-01.htm)

```json
{
  "digest": "{{ .WSSEPasswordDigest `{nonce}` `{created}` `{password}` }}"
}
```

### Section 'Remember'

Similar to `args` section, specifies plaseholder values for future reference (within test case scope).

The difference is that values for placeholders are taken from response (syntax is similar to `expect` matchers).

There are two types of sources for values to remember: response body and headers.

```json
{
  "remember": {
    "bodyPath": {
      "createdId": "path.to.id"
    },
    "headers": {
      "loc": "Location"
    }
  }
}
```

This section allowes more complex test scenarios like:

- 'request login token, remember, then use remembered {token} to request some data and verify'
- 'create resource, remember resource id from response, then use remembered {id} to delete resource'

### Using environment and context variables in tests

Similar to `args` and `remember` sections, OS environment variables could be used as placeholder values for future reference (within test case scope).

Given `MY_FILTER` environment variable exists in terminal session, the following syntax with `env` prefix enables its usage

```json
{
  "on": {
    "url": "http://example.com",
    "method": "GET",
    "params": {
      "filter": "{env:MY_FILTER}"
    }
  }
}
```

Context variables are available with `ctx` prefix

List of context variables

| Name        | Value                                                                       |
| ----------- | --------------------------------------------------------------------------- |
| base_url    | Base URL prefix for test calls. Command line argument provided with -H key  |


```json
{
  "expect" : {
    "bodyPath": {
      "_links.delete" : "{ctx:base_url}/my-resource/123"
    } 
  }
}
```


## Editor integration

To make work with test files convenient, we suggest to configure you text editors to use [this](./assets/test.schema.json) json schema. In this case editor will suggest what fields are available and highlight misspells.

| Editor             | JSON Autocomplete                                                                          |
| ------------------ | ------------------------------------------------------------------------------------------ |
| JetBrains Tools    | [native support](https://www.jetbrains.com/help/webstorm/2016.1/json-schema.html?page=1)   |
| Visual Studio Code | [native support](https://code.visualstudio.com/docs/languages/json#_json-schemas-settings) |
| Vim                | [plugin](https://github.com/Quramy/vison)                                                  |
