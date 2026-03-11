from flask import Blueprint, jsonify, render_template, request, redirect, url_for, flash
from scheduler import (
    get_scheduler,
    worker_configs,
)
from infra.sqlite.storage import SQLiteStorage
from models.WorkerStatus import WorkerStatus
from utils.mapper import to_dict
from config import WORKERS
import threading
import importlib
import re

workers_bp = Blueprint("workers", __name__)
worker_status_storage = SQLiteStorage("ningo_memory.db", WorkerStatus)


def get_worker_status_dict():
    # Retorna dict {job_name: status_dict} para uso nas rotas
    statuses = worker_status_storage.get_filtered()
    return {s.job_name: to_dict(s) for s in statuses}


def camel_to_snake(name):
    s1 = re.sub("(.)([A-Z][a-z]+)", r"\1_\2", name)
    return re.sub("([a-z0-9])([A-Z])", r"\1_\2", s1).lower()


def get_worker_config_by_id(worker_id):
    for config in WORKERS:
        sched_params = config.get("scheduler_params", {})
        if sched_params.get("id") == worker_id:
            return config
    return None


def get_worker_config_by_class(class_name):
    for config in WORKERS:
        worker_cls = config.get("worker_class")
        if worker_cls and worker_cls.__name__ == class_name:
            return config
    return None


@workers_bp.route("/health")
def health():
    """Retorna status em JSON para APIs"""
    worker_status = get_worker_status_dict()
    all_statuses = [info["status"] for info in worker_status.values()]
    overall_status = "ok" if all(s == "success" for s in all_statuses) else "degraded"
    return jsonify({"status": overall_status, "workers": worker_status})


@workers_bp.route("/workers")
def index():
    """Página web mostrando status de todos os workers"""
    worker_status = get_worker_status_dict()
    return render_template("workers.html", workers=worker_status)


@workers_bp.route("/worker/<worker_id>/pause", methods=["POST"])
def pause_worker(worker_id):
    scheduler = get_scheduler()
    try:
        scheduler.pause_job(worker_id)
        return jsonify({"success": True, "message": f"Worker {worker_id} paused."})
    except Exception as e:
        return jsonify({"success": False, "message": str(e)}), 400


@workers_bp.route("/worker/<worker_id>/resume", methods=["POST"])
def resume_worker(worker_id):
    scheduler = get_scheduler()
    try:
        scheduler.resume_job(worker_id)
        return jsonify({"success": True, "message": f"Worker {worker_id} resumed."})
    except Exception as e:
        return jsonify({"success": False, "message": str(e)}), 400


@workers_bp.route("/worker/<worker_id>/restart", methods=["POST"])
def restart_worker(worker_id):
    scheduler = get_scheduler()
    job = scheduler.get_job(worker_id)
    if not job:
        return jsonify({"success": False, "message": "Worker not found."}), 404
    try:
        # Remove and re-add the job with the same parameters
        job_args = job.args
        job_kwargs = job.kwargs
        job_func = job.func
        job_trigger = job.trigger
        job_id = job.id
        job_name = job.name
        job_trigger_args = (
            job.trigger.__getstate__() if hasattr(job.trigger, "__getstate__") else {}
        )
        scheduler.remove_job(worker_id)
        scheduler.add_job(
            job_func,
            trigger=job_trigger,
            args=job_args,
            kwargs=job_kwargs,
            id=job_id,
            name=job_name,
            **job_trigger_args,
        )
        return jsonify({"success": True, "message": f"Worker {worker_id} restarted."})
    except Exception as e:
        return jsonify({"success": False, "message": str(e)}), 400


@workers_bp.route("/worker/<worker_id>/execute_now", methods=["POST"])
def execute_now_worker(worker_id):
    config = worker_configs.get(worker_id)
    if not config:
        # Tenta buscar config pelo id no WORKERS
        config = get_worker_config_by_id(worker_id)
    if not config:
        # Tenta instanciar por reflexão: espera formato ClassName.func
        try:
            if "." not in worker_id:
                raise ValueError("worker_id deve ser no formato ClassName.func")
            class_name, func_name = worker_id.split(".", 1)
            module_name = f"workers.{camel_to_snake(class_name)}"
            module = importlib.import_module(module_name)
            worker_cls = getattr(module, class_name)
            # Busca config pelo nome da classe
            config_by_class = get_worker_config_by_class(class_name)
            if config_by_class:
                init_args = config_by_class.get("init_args", [])
                init_kwargs = config_by_class.get("init_kwargs", {})
                worker_instance = worker_cls(*init_args, **init_kwargs)
            else:
                worker_instance = worker_cls()
            func = getattr(worker_instance, func_name)

            def run_in_background():
                try:
                    func()
                except Exception as e:
                    pass

            threading.Thread(target=run_in_background, daemon=True).start()
            return jsonify(
                {
                    "success": True,
                    "message": f"Worker {worker_id} executed now (reflection).",
                }
            )
        except Exception as e:
            return (
                jsonify({"success": False, "message": f"Reflection error: {str(e)}"}),
                404,
            )
    try:
        worker_class = config["worker_class"]
        init_args = config.get("init_args", [])
        init_kwargs = config.get("init_kwargs", {})
        worker_instance = worker_class(*init_args, **init_kwargs)

        def run_in_background():
            try:
                worker_instance.run()
            except Exception as e:
                pass

        threading.Thread(target=run_in_background, daemon=True).start()
        return jsonify(
            {"success": True, "message": f"Worker {worker_id} executed now."}
        )
    except Exception as e:
        return jsonify({"success": False, "message": str(e)}), 400


@workers_bp.route("/workers/config", methods=["GET"])
def config_workers():
    worker_status = get_worker_status_dict()
    # Adiciona scheduler_params vindos do WORKERS
    for ws in worker_status.values():
        config = get_worker_config_by_id(ws["job_name"])
        ws["scheduler_params"] = config["scheduler_params"] if config else ""
    return render_template("worker_config.html", workers=worker_status)


@workers_bp.route("/workers/config/<worker_id>", methods=["POST"])
def update_worker_config(worker_id):
    trigger = request.form.get("trigger")
    params = request.form.get("params")
    scheduler = get_scheduler()
    job = scheduler.get_job(worker_id)
    if not job:
        flash("Worker não encontrado.", "error")
        return redirect(url_for("workers.config_workers"))
    # Remove e recria o job com novos parâmetros
    try:
        # Parse params (ex: seconds=60, minutes=5, etc)
        param_dict = {}
        for part in params.split(","):
            if "=" in part:
                k, v = part.split("=")
                param_dict[k.strip()] = (
                    int(v.strip()) if v.strip().isdigit() else v.strip()
                )
        job_func = job.func
        job_args = job.args
        job_kwargs = job.kwargs
        job_id = job.id
        job_name = job.name
        scheduler.remove_job(job_id)
        if trigger == "interval":
            scheduler.add_job(
                job_func,
                trigger="interval",
                id=job_id,
                name=job_name,
                args=job_args,
                kwargs=job_kwargs,
                **param_dict,
            )
        elif trigger == "cron":
            scheduler.add_job(
                job_func,
                trigger="cron",
                id=job_id,
                name=job_name,
                args=job_args,
                kwargs=job_kwargs,
                **param_dict,
            )
        # Atualiza config no WORKERS (em memória)
        config = get_worker_config_by_id(worker_id)
        if config:
            config["scheduler_params"].update({"trigger": trigger, **param_dict})
        flash("Configuração atualizada com sucesso!", "success")
    except Exception as e:
        flash(f"Erro ao atualizar: {str(e)}", "error")
    return redirect(url_for("workers.config_workers"))
