$ErrorActionPreference = "Continue"
$dir = "F:\cc_demos\ua_hda\gohda"
$p = Start-Process -FilePath "$dir\hda.exe" -WorkingDirectory $dir -PassThru `
     -RedirectStandardOutput "$dir\hda_run.log" -RedirectStandardError "$dir\hda_err.log"

$samples = New-Object System.Collections.Generic.List[object]
while (-not $p.HasExited) {
    $pp = Get-Process -Id $p.Id -ErrorAction SilentlyContinue
    if ($pp) {
        $samples.Add([pscustomobject]@{ t = (Get-Date); cpu = $pp.CPU; ws = $pp.WorkingSet64; priv = $pp.PrivateMemorySize64 })
    }
    Start-Sleep -Milliseconds 400
}
# 结束前再采一次
$pp = Get-Process -Id $p.Id -ErrorAction SilentlyContinue
if ($pp) { $samples.Add([pscustomobject]@{ t = (Get-Date); cpu = $pp.CPU; ws = $pp.WorkingSet64; priv = $pp.PrivateMemorySize64 }) }

$n = $samples.Count
if ($n -lt 2) { Write-Output "样本不足"; exit }
$first = $samples[0]; $last = $samples[-1]
$dur = ($last.t - $first.t).TotalSeconds
$cpuInc = [double]$last.cpu - [double]$first.cpu
$cores = [Environment]::ProcessorCount
$avgPct = $cpuInc / $dur * 100.0
$peakWS = ($samples | Measure-Object -Property ws -Maximum).Maximum
$peakPriv = ($samples | Measure-Object -Property priv -Maximum).Maximum

# 瞬时 CPU% 峰值(相邻样本差分)
$maxRate = 0.0
for ($i = 1; $i -lt $n; $i++) {
    $dt = ($samples[$i].t - $samples[$i-1].t).TotalSeconds
    if ($dt -gt 0) {
        $rate = ([double]$samples[$i].cpu - [double]$samples[$i-1].cpu) / $dt * 100.0
        if ($rate -gt $maxRate) { $maxRate = $rate }
    }
}

Write-Output ("运行时长: {0:N1}s" -f $dur)
Write-Output ("CPU 累计增量: {0:N2}s, 本机核数: {1}" -f $cpuInc, $cores)
Write-Output ("CPU 平均占用: {0:N1}% (折合单核 {1:N1}%)" -f $avgPct, ($avgPct/$cores))
Write-Output ("CPU 瞬时峰值: {0:N1}%" -f $maxRate)
Write-Output ("内存峰值 工作集: {0:N1} MB / 私有: {1:N1} MB" -f ($peakWS/1MB), ($peakPriv/1MB))
Write-Output "--- 程序输出 ---"
Get-Content "$dir\hda_run.log" -ErrorAction SilentlyContinue
Get-Content "$dir\hda_err.log" -ErrorAction SilentlyContinue
