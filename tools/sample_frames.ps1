param([int]$Seconds = 300, [int]$Interval = 5)
$url = 'http://127.0.0.1:9501/metrics'
function Get-DataFrames {
    $text = (Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 8).Content
    if ($text -match 'kind="data"\} (\d+)') { return [double]$matches[1] }
    return -1
}
$prev = Get-DataFrames
$lines = @()
$elapsed = 0
while ($elapsed -lt $Seconds) {
    Start-Sleep -Seconds $Interval
    $elapsed += $Interval
    $cur = Get-DataFrames
    if ($prev -ge 0 -and $cur -ge 0) {
        $rate = ($cur - $prev) / $Interval
        $lines += ('{0:HH:mm:ss} {1:N1}/s' -f (Get-Date), $rate)
    }
    $prev = $cur
}
$lines | Set-Content 'F:\rf4_src\logs\frame_rate.txt' -Encoding utf8
'ok'
