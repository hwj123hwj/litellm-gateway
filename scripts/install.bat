@echo off
chcp 65001 >nul 2>&1
setlocal enabledelayedexpansion

:: ============================================================
::  LLM Gateway Windows Installer
::  用法: install.bat
::  下载地址: curl -o install.bat https://raw.githubusercontent.com/hwj123hwj/litellm-gateway/main/scripts/install.bat && install.bat
:: ============================================================

set "REPO=hwj123hwj/litellm-gateway"
set "INSTALL_ROOT=%USERPROFILE%\.llm-gateway"
set "INSTALL_DIR=%INSTALL_ROOT%\bin"
set "BINARY_NAME=gateway.exe"
set "CONFIG_FILE=%INSTALL_ROOT%\.env"
set "PROVIDERS_FILE=%INSTALL_ROOT%\providers.yaml"

echo.
echo   ╔══════════════════════════════════════╗
echo   ║      LLM Gateway Installer          ║
echo   ║   轻量级 LLM API 网关 (18MB 内存)   ║
echo   ╚══════════════════════════════════════╝
echo.

:: ── 1. 检测环境 ──
echo   ℹ  检测系统环境...
echo   ℹ  平台: windows/amd64

:: 检查 curl 是否可用
curl --version >nul 2>&1
if errorlevel 1 (
    echo   ✗ 未找到 curl，需要 Windows 10 1803 或更高版本
    echo   ✗ 请手动下载: https://github.com/%REPO%/releases/latest
    pause
    exit /b 1
)

:: ── 2. 创建安装目录 ──
if not exist "%INSTALL_DIR%" (
    mkdir "%INSTALL_DIR%"
    echo   ✓  创建目录: %INSTALL_DIR%
) else (
    echo   ✓  目录已存在: %INSTALL_DIR%
)

set "BINARY_PATH=%INSTALL_DIR%\%BINARY_NAME%"

:: ── 3. 检查已安装版本 ──
if exist "%BINARY_PATH%" (
    echo   ℹ  发现已安装的 gateway
    set /p "REPLY=  ℹ  重新安装/升级? [Y/n]: "
    if /i "!REPLY!"=="n" (
        echo   ✓  保留现有安装
        goto :skip_download
    )
)

:: ── 4. 获取最新版本并下载 ──
echo   ℹ  获取最新版本...

:: 获取最新 release tag
for /f "tokens=2 delims=:, " %%a in ('curl -fsSL "https://api.github.com/repos/%REPO%/releases/latest" 2^>nul ^| findstr /c:"tag_name"') do (
    set "TAG=%%~a"
)
:: 去掉引号
set "TAG=!TAG:"=!"

if "!TAG!"=="" (
    echo   ⚠  无法获取版本号，尝试从源码编译...
    goto :compile_source
)

echo   ℹ  下载 gateway !TAG!...

set "URL=https://github.com/%REPO%/releases/download/!TAG!/gateway-windows-amd64.exe"
curl -fSL "!URL!" -o "%BINARY_PATH%.tmp" >nul 2>&1
if errorlevel 1 (
    echo   ⚠  下载预编译失败，尝试从源码编译...
    goto :compile_source
)

move /y "%BINARY_PATH%.tmp" "%BINARY_PATH%" >nul
echo   ✓  下载完成 !TAG!
goto :skip_download

:compile_source
:: 回退：从源码编译（需要 Go 环境）
where go >nul 2>&1
if errorlevel 1 (
    echo   ✗  无法下载二进制且未安装 Go
    echo   ✗  请安装 Go: https://go.dev/dl/ 后重新运行此脚本
    pause
    exit /b 1
)

echo   ℹ  从源码编译（检测到 Go 环境）...
set "TMPDIR=%TEMP%\llm-gateway-build-%RANDOM%"
git clone --depth 1 "https://github.com/%REPO%.git" "!TMPDIR!\llm-gateway" >nul 2>&1
if errorlevel 1 (
    echo   ✗  克隆仓库失败
    pause
    exit /b 1
)
cd /d "!TMPDIR!\llm-gateway\go-gateway"
set "CGO_ENABLED=0"
go build -ldflags "-s -w" -o "%BINARY_PATH%" .
cd /d "%~dp0"
rd /s /q "!TMPDIR!" >nul 2>&1
echo   ✓  编译完成

:skip_download
echo   ✓  二进制: %BINARY_PATH%

:: ── 5. 配置 PATH ──
echo   ℹ  检查 PATH 配置...

:: 检查当前 session 的 PATH 是否已包含
echo ";%PATH%;" | findstr /i /c:";%INSTALL_DIR%;" >nul
if not errorlevel 1 (
    echo   ✓  PATH 已配置
    goto :path_done
)

:: 检查用户永久 PATH 是否已包含
for /f "tokens=2*" %%a in ('reg query "HKCU\Environment" /v Path 2^>nul') do (
    echo ";%%b;" | findstr /i /c:";%INSTALL_DIR%;" >nul
    if not errorlevel 1 (
        echo   ✓  PATH 已配置（永久）
        goto :path_done
    )
)

:: 添加到永久 PATH
echo   ℹ  添加到 PATH...
:: 先读取当前用户 PATH
for /f "tokens=2*" %%a in ('reg query "HKCU\Environment" /v Path 2^>nul') do (
    set "CURRENT_PATH=%%b"
)
if "!CURRENT_PATH!"=="" (
    set "NEW_PATH=%INSTALL_DIR%"
) else (
    set "NEW_PATH=!CURRENT_PATH!;%INSTALL_DIR%"
)
setx PATH "!NEW_PATH!" >nul 2>&1
:: 也加到当前 session
set "PATH=%PATH%;%INSTALL_DIR%"
echo   ✓  PATH 已配置（永久，新终端生效）

:path_done

:: ── 6. 下载 providers.yaml ──
if not exist "%PROVIDERS_FILE%" (
    echo   ℹ  下载 providers.yaml...
    curl -fsSL "https://raw.githubusercontent.com/%REPO%/main/go-gateway/providers.yaml" -o "%PROVIDERS_FILE%" >nul 2>&1
    if errorlevel 1 (
        echo   ⚠  providers.yaml 下载失败，稍后可手动复制
    ) else (
        echo   ✓  providers.yaml 已下载
    )
) else (
    echo   ✓  providers.yaml 已存在
)

:: ── 7. 配置 API Key ──
if not exist "%CONFIG_FILE%" (
    echo.
    echo   ── 配置 LLM Gateway ──
    echo.
    echo   选择要启用的提供商（输入数字，多个用空格分隔）:
    echo   1) 智谱 GLM
    echo   2) 小米 MiMo
    echo   3) 美团 LongCat
    echo   4) EasyClaw
    echo   5) 跳过，稍后配置
    echo.
    set /p "CHOICES=  ℹ  选择 [1 2 3 4 / 5]: "

    set "GLM_KEY="
    set "MIMO_KEY="
    set "LONGCAT_KEY="
    set "EASYCLAW_KEY="

    echo !CHOICES! | findstr /c:"5" >nul
    if not errorlevel 1 goto :skip_keys

    for %%c in (!CHOICES!) do (
        if "%%c"=="1" (
            set /p "GLM_KEY=  ℹ  智谱 GLM API Key: "
        )
        if "%%c"=="2" (
            set /p "MIMO_KEY=  ℹ  小米 MiMo API Key: "
        )
        if "%%c"=="3" (
            set /p "LONGCAT_KEY=  ℹ  美团 LongCat API Key: "
        )
        if "%%c"=="4" (
            set /p "EASYCLAW_KEY=  ℹ  EasyClaw API Key: "
        )
    )

:skip_keys

    :: 生成随机 master key (32 字符)
    set "MASTER_KEY=sk-"
    for /L %%i in (1,1,32) do (
        set /a "R=!RANDOM! %% 62"
        set "CHARS=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
        for %%j in (!R!) do set "MASTER_KEY=!MASTER_KEY!!CHARS:~%%j,1!"
    )

    (
        echo # LLM Gateway 配置文件
        echo # 网关认证 token（自动生成，用于 Claude Code 等客户端连接）
        echo LITELLM_MASTER_KEY=!MASTER_KEY!
        echo.
        echo # 提供商 API Key（按需填写）
        echo GLM_API_KEY=!GLM_KEY!
        echo MIMO_API_KEY=!MIMO_KEY!
        echo LONGCAT_API_KEY=!LONGCAT_KEY!
        echo EASYCLAW_API_KEY=!EASYCLAW_KEY!
        echo.
        echo # 网关端口
        echo PORT=4001
        echo.
        echo # 日志级别
        echo LOG_LEVEL=info
    ) > "%CONFIG_FILE%"

    echo   ✓  配置已保存（Master Key 已自动生成）
) else (
    echo   ✓  配置已存在: %CONFIG_FILE%
)

:: ── 8. 创建快捷启动脚本 ──
set "LAUNCHER=%INSTALL_DIR%\llm-gateway.cmd"
(
    echo @echo off
    echo cd /d "%INSTALL_ROOT%"
    echo "%BINARY_PATH%" %%*
) > "%LAUNCHER%"
echo   ✓  启动器: %LAUNCHER%

:: ── 9. 完成 ──
echo.
echo   🎉 安装完成！
echo.
echo   启动网关:
echo      llm-gateway
echo.
echo   配置 Claude Code:
echo      编辑 %%USERPROFILE%%\.claude\settings.json：
echo      {
echo        "env": {
echo          "ANTHROPIC_BASE_URL": "http://localhost:4001/v1",
echo          "ANTHROPIC_AUTH_TOKEN": "你的 LITELLM_MASTER_KEY"
echo        }
echo      }
echo.
echo   修改配置:
echo      notepad %CONFIG_FILE%
echo.
echo   修改提供商:
echo      notepad %PROVIDERS_FILE%
echo.
echo   安装路径: %INSTALL_DIR%
echo   配置文件: %CONFIG_FILE%
echo.
echo   提示: 请重新打开 CMD 窗口使 PATH 生效
echo.
pause
