# keep-sorted

![go workflow status](https://github.com/google/keep-sorted/actions/workflows/go.yml/badge.svg)
![pre-commit workflow status](https://github.com/google/keep-sorted/actions/workflows/pre-commit.yml/badge.svg)

keep-sorted is a language-agnostic formatter that sorts lines between two
markers in a larger file.

## Usage

Surround the lines to keep sorted with `keep-sorted start` and
`keep-sorted end` in comments. For example, in Java:

<table border="0">
<tr>
<td>
<b>Before</b>

```java
@Component(
    modules = {

      UtilsModule.class,
      GetRequestModule.class,
      PostRequestModule.class,
      AuthModule.class,
      MonitoringModule.class,
      LoggingModule.class,

    })
interface FrontendComponent {
  FrontendRequestHandler requestHandler();
}
```
</td>
<td>
<b>After</b>

```diff
@Component(
    modules = {
+     // keep-sorted start
      AuthModule.class,
      GetRequestModule.class,
      LoggingModule.class,
      MonitoringModule.class,
      PostRequestModule.class,
      UtilsModule.class,
+     // keep-sorted end
    })
interface FrontendComponent {
  FrontendRequestHandler requestHandler();
}
```
</td>
</tr>
</table>

You can also nest keep-sorted blocks:

<table border="0">
<tr>
<td>

<!-- Including a long blank line here so that the code block width is more
consistent -->
```python
                              
foo = [

  'y',
  'x',
  'z',

]
bar = [

  '1',
  '3',
  '2',

]

```
</td>
<td>

```diff
+# keep-sorted start block=yes
 bar = [
+  # keep-sorted start
   '1',
   '2',
   '3',
+  # keep-sorted end
 ]
 foo = [
+  # keep-sorted start
   'x',
   'y',
   'z',
+  # keep-sorted end
 ]
+# keep-sorted end
```
</td>
</tr>
</table>

### Sorting your file

1. Install go: https://go.dev/doc/install

> [!NOTE]
> keep-sorted currently requires at least go 1.23.

2. Install keep-sorted:

   ```sh
   $ go install github.com/google/keep-sorted@v0.7.1
   ```

3. Run keep-sorted:

   ```sh
   $ keep-sorted [file1] [file2] ...
   ```

   If the file is `-`, the tool will read from stdin and write to stdout.

#### pre-commit

You can run keep-sorted automatically by adding this repository to your
[pre-commit](https://pre-commit.com/).

```yaml
- repo: https://github.com/google/keep-sorted
  rev: v0.7.1
  hooks:
    - id: keep-sorted
```


## Options

### Pre-sorting options

Pre-sorting options tell keep-sorted what content in your file constitutes a
single logical line that needs to be sorted.

#### Line continuations

By default, keep-sorted will interpret increasing indentation as a line
continuation and group indented lines with the lines above. If you don't want
this behavior, line continuation can be disabled via `group=no`.

<table border="0">
<tr>
<td>

```java
// keep-sorted start
private final Bar bar;
private final Baz baz =
    new Baz()
private final Foo foo;
// keep-sorted end
```

</td>
<td>

```diff
+// keep-sorted start group=no
     new Baz()
 private final Bar bar;
 private final Baz baz =
 private final Foo foo;
 // keep-sorted end
```

</td>
</tr>
</table>

</section>

#### Blocks

Alternatively, `block=yes` is an opt-in way to handle more complicated blocks of
code, with some gotchas. It looks at characters that are typically expected to
be closed in a single logical line of code (e.g., braces are balanced). Thus,
what gets considered a group is the smallest set of lines that has all the
typical symbols balanced (parentheses, braces, brackets, and quotes). This
allows for sorting data such as Go structs and JSON objects.

<table border="0">
<tr>
<td>

```go
  widgets := []widget{

    {
      Name: "def",
    },
    {
      Name: "abc",
    },

  }
```

</td>
<td>

```diff
  widgets := []widget{
+   // keep-sorted start block=yes
    {
      Name: "abc",
    },
    {
      Name: "def",
    },
+   // keep-sorted end
  }
```

</td>
</tr>
</table>

> Warning: keep-sorted is not language aware, so the groups are still being
> sorted as basic strings. e.g., "{\n" comes before "{Name:", so mixing the
> line break and whitespace usage may cause unexpected sorting.

> Note: Braces within things that look like string literals are **not** counted
> when pairing braces. A string literal begins an ends with a matching pair of
> quotes, where quotes can be any of the following:
> ````
> '
> '''
> "
> """
> `
> ```
> ````

> Note: angle brackets (`<` and `>`) are not supported by block mode due to
> being used for mathematical expressions in an unbalanced format.

#### Custom grouping

Another way to group lines together is with the `group_prefixes` option. This
takes a comma-separated list of prefixes. Any line beginning with one of those
prefixes will be treated as a continuation line.

<table border="0">
<tr>
<td>

```

spaghetti
with meatballs
peanut butter
and jelly
hamburger
with lettuce
and tomatoes

```

</td>
<td>

```diff
+// keep-sorted start group_prefixes=and,with
 hamburger
 with lettuce
 and tomatoes
 peanut butter
 and jelly
 spaghetti
 with meatballs
+// keep-sorted end
```

</td>
</tr>
</table>

#### Comments

Comments embedded within the sorted block are made to stick with their
successor. The comment lines must start with the same comment marker as the
keep-sorted instruction itself (e.g. `#` in the case below). keep-sorted
will recognize `//`, `/*`, `#`, `--`, `;`, and `<!--` as comment markers, for
any other kinds of comments, use `sticky_prefixes`.

This special handling can be disabled by specifying the parameter
`sticky_comments=no`:

<table border="0">
<tr>
<td>

```textproto
# keep-sorted start
# alice
username: al1
# bob
username: bo2
# charlie
username: ch3
# keep-sorted end
```

</td>
<td>

```diff
+# keep-sorted start sticky_comments=no
# alice
# bob
# charlie
username: al1
username: bo2
username: ch3
 # keep-sorted end
```

</td>
</tr>
</table>

More prefixes can be made to stick with their successor. The option
`sticky_prefixes` takes a comma-separated list of prefixes that will all be
treated as sticky. These prefixes cannot contain space characters.

```diff
+// keep-sorted start sticky_prefixes=/*,@Annotation
 Baz baz;
 /* Foo */
 @Annotation
 Foo foo;
 // keep-sorted end
```

#### Skipping lines

In some cases, it may not be possible to have the start directive on the line
immediately before the sorted region. In this case, `skip_lines` can be used to
indicate how many lines are to be skipped before the sorted region.

For instance, this can be used with a Markdown table, to prevent the headers
and the dashed line after the headers from being sorted:

<table border="0">
<tr>
<td>

```md

Name    | Value
------- | -----
Charlie | Baz
Delta   | Qux
Bravo   | Bar
Alpha   | Foo

```

</td>
<td>

```diff
+<!-- keep-sorted start skip_lines=2 -->
 Name    | Value
 ------- | -----
 Alpha   | Foo
 Bravo   | Bar
 Charlie | Baz
 Delta   | Qux
+<!-- keep-sorted end -->
```

</td>
</tr>
</table>

### Sorting options

Sorting options tell keep-sorted how the logical lines in your keep-sorted
block should be sorted.

#### Case sensitivity

By default, keep-sorted is case-sensitive. This means that uppercase letters
will be ordered before lowercase ones. This behavior can be changed to sort
case-insensitively using the `case` flag:

<table border="0">
<tr>
<td>

```proto
# keep-sorted start
Bravo
Delta
Foxtrot
alpha
charlie
echo
# keep-sorted end
```

</td>
<td>

```diff
+# keep-sorted start case=no
 alpha
 Bravo
 charlie
 Delta
 echo
 Foxtrot
 # keep-sorted end
```

</td>
</tr>
</table>

#### Numeric sorting

By default, keep-sorted uses lexical sorting. Depending on your data, this is
not what you might want. By specifying `numeric=yes`, sequences of digits
embedded in the lines are interpreted by their numeric values and sorted
accordingly:

<table border="0">
<tr>
<td>

```python
progress = (
  # keep-sorted start
  'PROGRESS_100_PERCENT',
  'PROGRESS_10_PERCENT',
  'PROGRESS_1_PERCENT',
  'PROGRESS_50_PERCENT',
  'PROGRESS_5_PERCENT',
  # keep-sorted end
)
```

</td>
<td>

```diff
progress = (
+ # keep-sorted start numeric=yes
  'PROGRESS_1_PERCENT',
  'PROGRESS_5_PERCENT',
  'PROGRESS_10_PERCENT',
  'PROGRESS_50_PERCENT',
  'PROGRESS_100_PERCENT',
  # keep-sorted end
)
```

</td>
</tr>
</table>

#### Regular expressions

It can be useful to sort an entire group based on a non-prefix substring. The
option `by_regex=…` takes a comma-separated list of [RE2 regular
expressions] that will be applied to the group, and then sorting
will take place on just the results of the regular expressions.

> [!TIP]
> Regular expressions often need special characters. See [Syntax](#syntax) below
> for how to include special characters in the `by_regex` option.

By default, all characters that the regular expression matches will be
considered for sorting. If the regular expression contains any capturing groups,
only the characters matched by the capturing groups will be considered for
sorting. The result from each regular expression will be concatenated into a
list of results, and that list of results will be sorted [lexicographically].

Regular expressions are applied **after** pre-sorting options.
[`group_prefixes`](#custom-grouping) will consider to the content of the file
before any regular expression has been applied to it.

Regular expressions are applied **before** other sorting options.
[`case`](#case-sensitivity), [`numeric`](#numeric-sorting), and
[`prefix_order`](#prefix-sorting) will only apply to the characters matched by
your regular expressions.

> [!TIP]
> If you want your regular expression itself to be case insensitive, consider
> setting the case-insensitive flag `(?i)` at the start of your expression.

When using the [YAML Sequence syntax](#syntax) as the argument, a [regexp template](https://pkg.go.dev/regexp#Regexp.Expand) can be supplied optionally as a single string-to-string mapping in the sequence.  Refer to the `Bernoulli` example below for a demonstration of pattern group reordering.  Note that a templated rewrite will be returned wholly as the sorting key.

[RE2 regular expressions]: https://github.com/google/re2/wiki/Syntax
[lexicographically]: https://en.wikipedia.org/wiki/Lexicographic_order

<table border="0">
<tr>
<td>

```java
// keep-sorted start
List<String> foo;
Object baz;
String bar;
// keep-sorted end
```

```java
// keep-sorted start
List<String> foo;
Object baz;
String bar;
// keep-sorted end
```

```java
// keep-sorted start block=yes newline_separated=yes case=no
bool func2() {
  return true;
}

int func1() {
  return 1;
}

List<SomeReallyLongTypeParameterThatWouldForceTheFunctionNameOnlyTheNextLine>
  func0() {
    return List.of(whatever);
}
// keep-sorted end
```

```
keep-sorted start skip_lines=1

Daniel Bernoulli
Emmy Noether
Jacob Bernoulli
Johann Bernoulli
Max Noether
Nicolaus Bernoulli

keep-sorted end
```

```
// keep-sorted start numeric=yes
Data Size A 20M
Data Size A 50K
Data Size A 250M
Data Size B 1B
Data Size B 80M
Data Size B 250K
// keep-sorted end
```

</td>
<td>

```diff
+// keep-sorted start by_regex=\w+;
 String bar;
 Object baz;
 List<String> foo;
 // keep-sorted end
```

```diff
+// keep-sorted start by_regex=\w+; prefix_order=foo
 List<String> foo;
 String bar;
 Object baz;
 // keep-sorted end
```

```diff
+// keep-sorted start block=yes newline_separated=yes case=no by_regex=(\w+)\(\)\s+{ numeric=yes
 List<SomeReallyLongTypeParameterThatWouldForceTheFunctionNameOnlyTheNextLine>
     func0() {
   return List.of(whatever);
 }
 
 int func1() {
   return 1;
 }
 
 bool func2() {
   return true;
 }
 // keep-sorted end
```

```diff
+keep-sorted start skip_lines=1 by_regex=['^(?<first_name>\w+) (?<last_name>\w+)$': '${last_name} ${first_name}']
 
 Daniel Bernoulli
 Jacob Bernoulli
 Johann Bernoulli
 Nicolaus Bernoulli
 Emmy Noether
 Max Noether
 
 keep-sorted end
```

```diff
+// keep-sorted start numeric=yes by_regex=(?i)(.*?)(?:(\d+)b|(\d+)m|(\d+)k)
Data Size A 50K
Data Size A 20M
Data Size A 250M
Data Size B 250K
Data Size B 80M
Data Size B 1B
// keep-sorted end
```

</td>
</tr>
</table>

#### Prefix sorting

Sometimes, it is useful to specify a custom ordering for some elements. The
option `prefix_order=…` takes a comma-separated list of prefixes that is
matched against the lines to be sorted: if the line starts with one of the
specified values, it is put at the corresponding position. Lines that don't
match any of the prefixes are put after any lines that have a matching prefix.
You can use an empty prefix to put unmatching lines in between non-empty
prefixes.

<table border="0">
<tr>
<td>

```c




// keep-sorted start
DO_SOMETHING_WITH_BAR,
DO_SOMETHING_WITH_FOO,
FINAL_BAR,
FINAL_FOO,
INIT_BAR,
INIT_FOO
// keep-sorted end
```

</td>
<td>

```diff
 // Keep this list sorted with
 //   - INIT_* first
 //   - FINAL_* last
 //   - Everything else in between
+// keep-sorted start prefix_order=INIT_,,FINAL_
 INIT_BAR,
 INIT_FOO,
 DO_SOMETHING_WITH_BAR,
 DO_SOMETHING_WITH_FOO,
 FINAL_BAR,
 FINAL_FOO
 // keep-sorted end
```

</td>
</tr>
</table>

This can also be combined with numeric sorting:

```diff
droid_components = [
+ # keep-sorted start numeric=yes prefix_order=R2,C3
  R2D2_BOLTS_5_MM,
  R2D2_BOLTS_10_MM,
  R2D2_PROJECTOR,
  C3PO_ARM_L,
  C3PO_ARM_R,
  C3PO_HEAD,
  R4_MOTIVATOR,
  # keep-sorted end
]
```

#### Ignore prefixes

For some use cases, there are prefix strings that would be best ignored when
trying to keep items in an order. The option `ignore_prefixes=…` takes a
comma-separated list of prefixes that are ignored for sorting purposes. If the
line starts with any or no whitespace followed by one of the listed prefixes,
the prefix is treated as the empty string for sorting purposes.

<table border="0">
<tr>
<td>

```go
// keep-sorted start
fs.setBoolFlag("paws_with_cute_toebeans", true)
fs.setBoolFlag("whiskered_adorable_dog", true)
fs.setIntFlag("pretty_whiskered_kitten", 6)
// keep-sorted end
```

</td>
<td>

```diff
+// keep-sorted start ignore_prefixes=fs.setBoolFlag,fs.setIntFlag
 fs.setBoolFlag("paws_with_cute_toebeans", true)
 fs.setIntFlag("pretty_whiskered_kitten", 6)
 fs.setBoolFlag("whiskered_adorable_dog", true)
 // keep-sorted end
```

</td>
</tr>
</table>

This can also be combined with numerical sorting:

```diff
 droid_components = [
+  # keep-sorted start numeric=yes ignore_prefixes=R2D2,C3PO,R4
   C3PO_ARM_L,
   C3PO_ARM_R,
   R2D2_BOLTS_5_MM,
   R2D2_BOLTS_10_MM,
   C3PO_HEAD,
   R4_MOTIVATOR,
   R2D2_PROJECTOR,
   # keep-sorted end
 ]
```

### Post-sorting options

Post-sorting options are additional convenience features that make the resulting
code more readable.

#### Duplicates

By default, keep-sorted removes duplicates from the sorted section. If
different [comments are attached](#comments) to otherwise identical lines, the
entries are preserved:

```textproto
# keep-sorted start
rotation: bar
# Add bar twice!
rotation: bar
rotation: baz
rotation: foo
# keep-sorted end
```

The duplicate handling can be changed with the switch `remove_duplicates`:

```diff
+# keep-sorted start remove_duplicates=no
 rotation: bar
 rotation: bar
 rotation: baz
 rotation: baz
 rotation: baz
 rotation: foo
 # keep-sorted end
```

#### Newline separated

There is also a `newline_separated=yes` option that can be used to add blank
lines between the items that keep-sorted is sorting:

<table border="0">
<tr>
<td>

```
# keep-sorted start
Apples
Bananas
Oranges
Pineapples
# keep-sorted end



```

</td>
<td>

```diff
+# keep-sorted start newline_separated=yes
 Apples
 
 Bananas
 
 Oranges
 
 Pineapples
 # keep-sorted end
```

</td>
</tr>
</table>

Set newline_separated=yes for a single blank line, or
newline_separated=N to separate items with N blank lines.

<table border="0">
<tr>
<td>

```
# keep-sorted start
Apples
Bananas
Oranges
Pineapples
# keep-sorted end



```

</td>
<td>

```diff
+# keep-sorted start newline_separated=2
 Apples
 
 
 Bananas
 
 
 Oranges
 
 
 Pineapples
 # keep-sorted end
```

</td>
</tr>
</table>

### Syntax

If you find yourself wanting to include special characters (spaces, commas, left
brackets) in a comma-separated list of one of the options, you can do so with a
YAML [flow sequence](https://yaml.org/spec/1.2.2/#flow-sequences).

```md
<!-- keep-sorted start prefix_order=["* ", "* ["] -->
  * bar
  * foo
  * [baz](path/to/baz)
<!-- keep-sorted end -->
```

This works for all options that accept multiple values.

## FAQ

### How does keep-sorted handle whitespace?

The goal is for keep-sorted to handle whitespace the same way a human would. For
instance, the default `group=yes` behavior groups lines of increasing
indentation together for sorting, the way most people would. keep-sorted also
doesn't consider leading whitespace when sorting strings.

keep-sorted does fall short in a couple areas, however. Unlike humans, perhaps,
keep-sorted preserves any number of trailing newlines.  For example, keep-sorted
will not remove the 4 trailing newlines in the following block:

```
keep-sorted start
1
2
3




keep-sorted end
```

Additionally, keep-sorted does not preserve other linebreaks like a person
might. All non-trailing linebreaks will be moved to the beginning of the content
(and deduplicated if `remove_duplicates=yes`):

<table border="0">
<tr>
<td>

```
                 
1

2

3

```

</td>
<td>

```
keep-sorted start

1
2
3

keep-sorted end
```

</td>
</tr>
</table>

### How does keep-sorted handle lists that aren't allowed to have trailing commas?

Some languages allow trailing commas in lists and some don't. Luckily,
keep-sorted tries to do the right thing and handle commas "correctly".

<table border="0">
<tr>
<td>

```
                 
3,
1,
2

```

</td>
<td>

```
keep-sorted start
1,
2,
3
keep-sorted end
```

</td>
</tr>
</table>
