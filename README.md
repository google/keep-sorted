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
<h5>Before</h5>

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
<h5>After</h5>

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

### Sorting your file

```sh
$ keep-sorted [file1] [file2] ...
```

If the file is `-`, the tool will read from stdin and write to stdout.

#### pre-commit

You can automatically run keep-sorted by adding this repository to your
[pre-commit](https://pre-commit.com/).

```yaml
- repo: https://github.com/google/keep-sorted
  rev: v0.1.0
  hooks:
    -id: keep-sorted
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

> Warning: for performance and simplicity reasons, this is extremely basic
> parsing and is fooled by things like unbalanced symbols in strings. As well,
> it's not language aware, so the groups are still being sorted as basic
> strings. e.g., "{\n" comes before "{Name:", so mixing the line break and
> whitespace usage may cause unexpected sorting.

> Note: angle brackets (`<` and `>`) are not supported by block mode due to
> being used for mathematical expressions in an unbalanced format.

#### Comments

Comments embedded within the sorted block are made to stick with their
successor. The comment lines must start with the same token as the
keep-sorted instruction itself (e.g. `#` in the case below).

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

More prefixes can be made to stick with their successor. The argument
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

#### Prefix sorting

Sometimes, it is useful to specify a custom ordering for some elements. The
argument `prefix_order=…` takes a comma-separated list of prefixes that is
matched against the lines to be sorted: if the line starts with one of the
specified values, it is put at the corresponding position. If an empty prefix is
specified, any line not covered by other prefixes is matched.

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
trying to keep items in an order. The argument `ignore_prefixes=…` takes a
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
