# Tables
## Kifu table
### Definition

* `p`: projection
* `x`: check

|attributeName|type|attr|var=KIFU|var=STEP||GSI:Created|GSI:Start|GSI:Sfen|GSI:Position|
|-|-|-|-|-|-|-|-|-|-|
|kifuId|S|PK|x|x||*|*|*|*|
|var|S|SK|x|x||*|*|*|*|
|userId|S|x|x|x||PK|PK|SK|p|
|createdTs|N|x|x| ||SK| | | |
|startTs|N|x|x| || |SK| | |
|sfen|S|x|x| || | |PK| |
|pos|S|x| |x|| | | |PK|
|kifu|B| |x| ||p|p| | |
|version|N| |x| ||p|p| | |
|stepNum|N| |x| || | | | |
|step|B| | |x|| | | | |
|seq|N| | |x|| | | |p|

### Values

* `kifuId`: Kifu ID
* `var`: variable descriptor. values: `KIFU`,`STEP:{seq}`
* `userId`: User ID
* `createdTs`: Created timestamp
* `startTs`: Game start timestamp
* `sfen`: SFEN formated Kifu
* `pos`: Signature of position(SFEN pos format)
* `kifu`: protobuf.Kifu
* `version`: Timestamp for optimistic locking
* `stempNum`: Number of moves
* `step`: protobuf.Step
* `seq`: Sequence number of moves
